plugins {
    base
    id("com.carbonblack.gradle-dockerized-wrapper") version "1.2.1"

    // Pinned versions of plugins used by subprojects
    id("com.jfrog.artifactory") version "4.15.1" apply false
    id("com.palantir.git-version") version "0.12.3" apply false
    id("com.bmuschko.docker-remote-api") version "6.4.0" apply false
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

val buildTask = tasks.named("build") {
	inputs.dir("cmd/cb-event-forwarder")
	inputs.dir("scripts/")
	inputs.files("cb-event-forwarder.rpm.spec", "MANIFEST*", "makefile")
    	val outputDir = File("${project.buildDir}/rpm")
	outputs.dir(outputDir)
	outputs.file("cb-event-forwarder")
	dependsOn("getDeps")
	dependsOn("runUnitTests")
	doLast {
		project.delete(outputDir)
		project.exec {
			environment("RPM_OUTPUT_DIR" , outputDir)
			commandLine = listOf("make", "rpm")
		}
	}
}

val depTask = tasks.register<Exec>("getDeps") {
	inputs.dir("cmd/cb-event-forwarder")
	val gomodPath = "${System.getenv("GOPATH")}/pkg/mod"
	val gomodFile = File(gomodPath)
	if (! gomodFile.exists()) {
	     gomodFile.mkdirs()
	}
	inputs.dir("cmd/cb-event-forwarder")
	inputs.dir(gomodFile)
	outputs.dir(gomodFile)
	inputs.files("go.sum", "go.mod")
	executable("make")
	args("getdeps")
}

val unitTestTask = tasks.register<Exec>("runUnitTests") {
	inputs.dir("cmd/cb-event-forwarder")
	outputs.dir("cmd/cb-event-forwarder")
	executable("make")
	args("unittest")
}

val criticTask = tasks.register<Exec>("criticizeCode") {
	executable("make")
	args("critic")
}
