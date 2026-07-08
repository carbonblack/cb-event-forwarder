import java.io.ByteArrayOutputStream
import com.palantir.gradle.gitversion.VersionDetails
val currentVersion: String by project


plugins {
    base
    id("com.palantir.git-version") version "0.12.2"
    id("com.carbonblack.gradle-dockerized-wrapper") version "1.2.1"
    // Pinned versions of plugins used by subprojects
    id("com.bmuschko.docker-remote-api") version "6.7.0" apply false
}

repositories {
    maven("https://usw1.packages.broadcom.com/artifactory/cb-maven-dev-virtual") {
        credentials {
            username = System.getenv("USW1_ACCESS_ID_DEV")
            password = System.getenv("USW1_ACCESS_TOKEN_DEV")
        }
    }
    maven("https://usw1.packages.broadcom.com/artifactory/cb-gradle-dev-virtual") {
        credentials {
            username = System.getenv("USW1_ACCESS_ID_DEV")
            password = System.getenv("USW1_ACCESS_TOKEN_DEV")
        }
    }
}

fun versionDetails(): VersionDetails {
    return (extensions.extraProperties.get("versionDetails") as? groovy.lang.Closure<*>)?.call() as VersionDetails
}

/** RPM release suffix stripped for module paths (e.g. 3.8.5-1 -> 3.8.5). */
fun baseProductVersionForModule(): String {
    val v = currentVersion.trim()
    val m = Regex("^(.+)-(\\d+)$").matchEntire(v)
    return if (m != null) m.groupValues[1] else v
}

/**
 * Modular repo name for EL8/EL9: cb-event-forwarder-release-<baseVer> on release branches, else cb-event-forwarder-develop
 * (aligned with cbent cb-edr-<branch> / cb-edr-develop).
 */
fun yumModuleNameForEventForwarder(): String {
    val baseVer = baseProductVersionForModule()
    val branch = try {
        versionDetails().branchName
    } catch (ignored: Exception) {
        System.getenv("GIT_BRANCH_LOCAL")?.replaceFirst("^origin/".toRegex(), "")
            ?: System.getenv("GIT_BRANCH")?.replaceFirst("^origin/".toRegex(), "")
            ?: "develop"
    }
    val bLower = branch.toLowerCase()
    return if ("release" in bLower) {
        "cb-event-forwarder-release-$baseVer"
    } else {
        "cb-event-forwarder-develop"
    }
}

/** Same Jenkins Build Timestamp env names as scripts/upload.py (optional). */
fun connectorBuildTimestampRaw(): String {
    val raw = System.getenv("CB_EDR_CONNECTOR_BUILD_TIMESTAMP")
        ?: System.getenv("cb_edr_connector_build_timestamp")
        ?: ""
    return raw.trim()
}

/** Pass through Jenkins `yyMMdd.HHmm`; empty or `local` means no timestamp segment for RPM version. */
fun normalizeConnectorTimestamp(ts: String): String {
    if (ts.isEmpty() || ts == "local") {
        return ""
    }
    return ts
}

/**
 * RPM Version / Makefile GIT_VERSION: [base from currentVersion] or [base].[timestamp] when Jenkins sets connector timestamp.
 * Matches cb-enterprise style (e.g. 7.9.2.260413.0951).
 */
fun rpmGitVersionForMake(): String {
    val base = baseProductVersionForModule()
    val ts = normalizeConnectorTimestamp(connectorBuildTimestampRaw())
    return if (ts.isNotEmpty()) {
        "$base.$ts"
    } else {
        base
    }
}

// This is running in a docker container so this value comes from the container OS.
val osVersionClassifier: String
    get() {
        return try {
            val versionText = File("/etc/redhat-release").readText()
            when {
                versionText.contains("release 9") -> "el9"
                versionText.contains("release 8") -> "el8"
                else -> "el7"
            }
        } catch (ignored: Exception) {
            "el7"
        }
    }

buildDir = file("build/$osVersionClassifier")
// GOPATH (and thus default GOMODCACHE) must not live under the module root: Go 1.17+ would treat
// GOPATH/pkg/mod/*.go as packages in this module and fail with invalid import paths / "go mod tidy".
val goBuildRoot =
    File(System.getProperty("java.io.tmpdir"), "cb-event-forwarder-go-$osVersionClassifier").absolutePath
val goPath = File(goBuildRoot, "gopath").absolutePath
val goModCache = File(goBuildRoot, "gomodcache").absolutePath
val goProxy = System.getenv()["GOPROXY"]
val rabbitMQSalt = System.getenv()["RABBITMQ_SALT"]

val depTask = tasks.register<Exec>("getDeps") {
    doFirst {
        // Legacy layout put GOPATH/pkg/mod inside the repo; remove it so the Go toolchain does not scan that tree as part of this module.
        // Remove any legacy GOPATH/pkg/mod inside the repo so Go doesn't scan it as part of this module.
        project.delete("$buildDir/gopath")
    }

    inputs.dir("cmd/cb-event-forwarder")
    inputs.dir("pkg")
    inputs.files("go.mod", "go.sum")
    outputs.dir(goModCache)

    if (goProxy != null) {
        environment("GOPROXY", goProxy)
    }
    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    executable("make")
    args("getdeps")
}

val protoGenerationTask = tasks.register<Exec>("compileProtobufs") {
    dependsOn(depTask)

    inputs.files("pkg/sensorevents/sensor_events.proto")
    outputs.files("pkg/sensorevents/sensor_events.pb.go")
    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    executable("make")
    args("compile-protobufs")
}


val inputJsonModels = "protobuf_json_structs.go"
val outputJsonModels = "protobuf_json_structs_easyjson.go"
val modelPackage = "pkg/protobufmessageprocessor/"
val jsonModelGenerationTask = tasks.register<Exec>("runEasyJson") {
    dependsOn(protoGenerationTask)

    inputs.files("$modelPackage/$inputJsonModels")
    outputs.files("$modelPackage/$outputJsonModels")
    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    executable("make")
    args("generateeasyjsonmodels")
}

val buildEventForwarderTask = tasks.register<Exec>("buildEventForwarder") {
    dependsOn(depTask)
    dependsOn(protoGenerationTask)
    dependsOn(jsonModelGenerationTask)

    val outputDir = File("${project.buildDir}/rpm")

    inputs.dir("cmd/cb-event-forwarder")
    inputs.dir("cmd/go-serviced")
    inputs.dir("pkg")
    inputs.dir("scripts/")
    inputs.files("cb-event-forwarder.rpm.spec", "MANIFEST*", "Makefile")
    outputs.dir(outputDir)

    doFirst {
        project.delete(outputDir)
    }

    environment("RPM_OUTPUT_DIR", outputDir)
    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    val rpmVer = rpmGitVersionForMake()
    environment("GIT_VERSION", rpmVer)
    environment("VERSION", rpmVer)
    environment("RABBITMQ_SALT", rabbitMQSalt ?: "")
    commandLine = listOf("make", "rpm")
}

/** EDRSERVER-722: assert systemd unit is 644 inside built RPM (skipped without rpm(8) or on EL6). */
val verifyEDRSERVER722RpmTask = tasks.register<Exec>("verifyEDRSERVER722Rpm") {
    dependsOn(buildEventForwarderTask)
    onlyIf {
        listOf("/bin/rpm", "/usr/bin/rpm").any { java.io.File(it).exists() }
    }
    workingDir(project.rootDir)
    executable("bash")
    args("scripts/verify_edrserver_722_rpm.sh", "${layout.buildDirectory.get().asFile}/rpm/RPMS/x86_64")
}

buildEventForwarderTask.configure {
    finalizedBy(verifyEDRSERVER722RpmTask)
}

val build = tasks.named("build").configure {
    dependsOn(buildEventForwarderTask)
}

val unitTestTask = tasks.register<Exec>("runUnitTests") {
    dependsOn(protoGenerationTask)
    dependsOn(depTask)
    dependsOn(buildEventForwarderTask)

    val unitTestResultsFile = File("$buildDir/unittest.out")

    inputs.dir("tests")
    inputs.dir("pkg")
    inputs.dir("test/raw_data")
    outputs.files(unitTestResultsFile)

    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    executable("go")
    args("test", "./tests")
    isIgnoreExitValue = true

    ByteArrayOutputStream().use { os ->
        standardOutput = os
        errorOutput = os

        doLast {
            os.writeTo(System.out)
            os.writeTo(unitTestResultsFile.outputStream())

            if (execResult?.exitValue != 0) {
                throw GradleException("Unit tests failed.")
            }
        }
    }
}

val criticTask = tasks.register<Exec>("criticizeCode") {
    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    executable("make")
    args("critic")
}

val buildEventForwarderDockerImageTask = tasks.register<Exec>("buildEventForwarderDockerImage") {
    dependsOn(buildEventForwarderTask)
    executable("docker")
    args("build", "./docker/", "--tag",
        "cb-pub-docker-dev-local2.usw1.packages.broadcom.com/cb-pub-docker-dev-local2/cb/event-forwarder:${versionDetails().branchName}-$currentVersion")

    doFirst {
        val v = rpmGitVersionForMake()
        val rpmName = "cb-event-forwarder-$v-1.$osVersionClassifier.x86_64.rpm"
        File("${project.buildDir}/rpm/RPMS/x86_64/$rpmName").copyTo(File("./docker/$rpmName"), true)
    }
}

val dockerLogin = tasks.register<Exec>("dockerLogin") {
    dependsOn(buildEventForwarderDockerImageTask)
    val user = System.getenv("ARTIFACTORY_USER")
    val pw = System.getenv("ARTIFACTORY_API_KEY")
    executable("docker")
    args("login", "-u", user, "-p", pw, "usw1.packages.broadcom.com")
}

val publishEventForwarderDockerImageTask = tasks.register<Exec>("publishEventForwarderDockerImageTask") {
    dependsOn(dockerLogin)
    dependsOn(buildEventForwarderDockerImageTask)
    executable("docker")
    args("push", "cb-pub-docker-dev-local2.usw1.packages.broadcom.com/cb-pub-docker-dev-local2/cb/event-forwarder:${versionDetails().branchName}-$currentVersion")
}

/** Jenkins default build step. Black Duck is not run unless RUN_BLACKDUCK_SCAN=true (set env + BLACKDUCK_API_TOKEN, etc.). */
tasks.register("buildJenkins").configure {
    val buildVersion = System.getenv("DOCKERIZED_BUILD_ENV")
    if(buildVersion == "centos8" || buildVersion == "rocky9") {
        dependsOn(buildEventForwarderTask)
    } else {
        dependsOn(publishEventForwarderDockerImageTask)
    }
    if (System.getenv("RUN_BLACKDUCK_SCAN")?.equals("true", ignoreCase = true) == true) {
        dependsOn(project(":blackduck").tasks.named("blackduckScan"))
    }
}

val osTypeForUpload: String
    get() = System.getenv("OS_CLASSIFIER")?.takeIf { it.isNotBlank() } ?: osVersionClassifier

val createYumRepoTask = tasks.register<Exec>("createYumRepo") {
    group = "distribution"
    description =
        "Create Yum metadata under rpm/RPMS/x86_64; EL8/EL9 also run repo2module + createrepo_c --update (CI when JENKINS_CI_VERSION is set)."
    dependsOn(buildEventForwarderTask)

    val rpmRoot = File(project.buildDir, "rpm/RPMS/x86_64")
    inputs.files(project.fileTree(rpmRoot).matching { include("*.rpm") })
    outputs.dir(File(rpmRoot, "repodata"))
    if (osVersionClassifier == "el8" || osVersionClassifier == "el9") {
        outputs.file(File(rpmRoot, "modules.yaml"))
    }

    // cbent-style: createrepo path from project dir; tool is fixed per OS (no runtime probing).
    workingDir(project.projectDir)
    val createrepoExe = if (osVersionClassifier == "el8" || osVersionClassifier == "el9") "createrepo_c" else "createrepo"
    executable(createrepoExe)
    args(rpmRoot.absolutePath)

    if (osVersionClassifier == "el8" || osVersionClassifier == "el9") {
        doLast {
            val moduleName = yumModuleNameForEventForwarder()
            logger.lifecycle("createYumRepo: repo2module --module-name {}", moduleName)
            val modulesYaml = File(rpmRoot, "modules.yaml")
            project.exec {
                workingDir(project.projectDir)
                commandLine(
                    "repo2module",
                    rpmRoot.absolutePath,
                    modulesYaml.absolutePath,
                    "--module-name",
                    moduleName
                )
            }
            project.exec {
                commandLine(
                    "sed",
                    "-i",
                    "/^  name:/,/^  summary:/s/^  summary:/  arch: x86_64\\n  summary:/",
                    modulesYaml.absolutePath
                )
            }
            project.exec {
                workingDir(rpmRoot)
                commandLine("createrepo_c", "--update", ".")
            }
        }
    }
}

val uploadTask = tasks.register<Exec>("upload") {
    group = "distribution"
    description = "Upload RPM + repodata via scripts/upload.py (jfrog CLI on PATH). Runs when JENKINS_CI_VERSION is set."
    dependsOn(createYumRepoTask)
    onlyIf { System.getenv("JENKINS_CI_VERSION") != null }

    workingDir(rootDir)
    commandLine(
        "python3",
        rootProject.file("scripts/upload.py").absolutePath,
        "--os-type",
        osTypeForUpload
    )
}

tasks.register("runJenkinsBuild") {
    group = "build"
    description = "CI: build + upload (upload only if JENKINS_CI_VERSION). Run before blackduckScan in Jenkins."
    dependsOn(tasks.named("build"))
    dependsOn(uploadTask)
}

val unitTestCoverageTask = tasks.register<Exec>("runUnitTestsCoverage") {
    dependsOn(depTask)
    dependsOn(protoGenerationTask)

    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    executable("make")
    args("unittest_coverage")

    isIgnoreExitValue = true

    ByteArrayOutputStream().use { os ->
        standardOutput = os
        errorOutput = os

        doLast {
            os.writeTo(System.out)
            if (execResult?.exitValue != 0) {
                throw GradleException("Unit tests Coverage failed.")
            }
        }
    }
 }

val integrationTestTask = tasks.register<Exec>("runIntegrationTests") {
    group = "verification"
    description = "Run integration tests (requires -tags integration build tag). " +
        "Tests exercise multiple components without external dependencies."
    dependsOn(depTask)
    dependsOn(protoGenerationTask)

    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    executable("make")
    args("integration_test")

    isIgnoreExitValue = true

    ByteArrayOutputStream().use { os ->
        standardOutput = os
        errorOutput = os

        doLast {
            os.writeTo(System.out)
            if (execResult?.exitValue != 0) {
                throw GradleException("Integration tests failed.")
            }
        }
    }
}

// Generate a single consolidated HTML report from whatever test data exists.
// OS_LABEL, GIT_BRANCH_LOCAL, and BUILD_NUMBER are injected by Jenkins;
// fall back to defaults locally.
val osLabel     = System.getenv("OS_LABEL") ?: "local"
val buildNumber = System.getenv("BUILD_NUMBER") ?: "0"
val gitBranch   = System.getenv("GIT_BRANCH_LOCAL")
    ?: System.getenv("GIT_BRANCH")
    ?: "local"

tasks.register<Exec>("generateTestReport") {
    group = "reporting"
    description = "Generate a single consolidated HTML test and coverage report " +
        "(build/test-report/report.html). All inputs are optional."
    dependsOn(depTask)

    environment("GOPATH", goPath)
    environment("GOMODCACHE", goModCache)
    executable("make")
    args("generate_report",
         "GIT_BRANCH=$gitBranch",
         "BUILD_NUMBER=$buildNumber")

    isIgnoreExitValue = true

    ByteArrayOutputStream().use { os ->
        standardOutput = os
        errorOutput = os

        doLast {
            os.writeTo(System.out)
        }
    }
}
