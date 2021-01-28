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
	dependsOn("formatCode")
	doLast {
    		val outputDir = File("${project.buildDir}/rpm")
		val gopath = System.getenv("GOPATH")
		project.delete(outputDir)
		project.exec {
			environment("GOPATH" , gopath)
			environment("RPM_OUTPUT_DIR" , outputDir)
			commandLine = listOf("make", "rpm")
		}
	}
}

val unitTestTask = tasks.register<Exec>("runUnitTests") {
	executable("go")
	args("test", "./cmd/cb-event-forwarder")
}

val formatTask = tasks.register<Exec>("formatCode") {
	val tree = fileTree("cmd/cb-event-forwarder") {
    		include("*.go")
	}
	val sourceFiles = tree.getFiles().map{ it -> "cmd/cb-event-forwarder/${it.name}"}
	val formatArgs = listOf("fmt") + sourceFiles
	executable("go")
	args(formatArgs)
}

val criticTask = tasks.register<Exec>("criticizeCode") {
	executable("make")
	args("critic")
}
