import java.io.ByteArrayOutputStream

plugins {
    base
    id("com.carbonblack.gradle-dockerized-wrapper") version "1.2.1"

    // Pinned versions of plugins used by subprojects
    id("com.bmuschko.docker-remote-api") version "6.7.0" apply false
}

// This is running in a docker container so this value comes from the container OS.
val osVersionClassifier: String
    get() {
        return try {
            val versionText = File("/etc/redhat-release").readText()
            when {
                versionText.contains("release 8") -> "el8"
                else -> "el7"
            }
        } catch (ignored: Exception) {
            "el7"
        }
    }

buildDir = file("build/$osVersionClassifier")
val goPath = "$buildDir/gopath"

val depTask = tasks.register<Exec>("getDeps") {
    val gomodPath = "$goPath/pkg/mod"

    inputs.dir("cmd/cb-event-forwarder")
    inputs.files("go.mod", "go.sum")
    outputs.dir(gomodPath)

    environment("GOPATH", goPath)
    executable("make")
    args("getdeps")
}

val protoGenerationTask = tasks.register<Exec>("compileProtobufs") {
    dependsOn(depTask)

    inputs.files("cmd/cb-event-forwarder/sensor_events.proto")
    outputs.files("cmd/cb-event-forwarder/sensor_events.pb.go")

    environment("GOPATH", goPath)
    executable("make")
    args("compile-protobufs")
}

val unitTestTask = tasks.register<Exec>("runUnitTests") {
    dependsOn(protoGenerationTask)
    dependsOn(depTask)

    val unitTestResultsFile = File("$buildDir/unittest.out")

    inputs.dir("cmd/cb-event-forwarder")
    inputs.dir("test/raw_data")
    outputs.files(unitTestResultsFile)

    environment("GOPATH", goPath)
    executable("go")
    args("test", "./cmd/cb-event-forwarder")
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
    executable("make")
    args("critic")
}

val buildEventForwarderTask = tasks.register<Exec>("buildEventForwarder") {
    dependsOn(depTask)
    dependsOn(protoGenerationTask)
    dependsOn(unitTestTask)

    val outputDir = File("${project.buildDir}/rpm")

    inputs.dir("cmd/cb-event-forwarder")
    inputs.dir("scripts/")
    inputs.files("cb-event-forwarder.rpm.spec", "MANIFEST*", "Makefile")
    outputs.dir(outputDir)

    doFirst {
        project.delete(outputDir)
    }

    environment("RPM_OUTPUT_DIR", outputDir)
    environment("GOPATH", goPath)
    commandLine = listOf("make", "rpm")
}

val buildTask = tasks.named("build").configure {
    dependsOn(buildEventForwarderTask)
}
