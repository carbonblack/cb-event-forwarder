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
val goPath = "$buildDir/gopath"
File(goPath).mkdirs()

val buildTask = tasks.named("build") {
	inputs.dir("cmd/cb-event-forwarder")
	inputs.dir("scripts/")
	inputs.files("cb-event-forwarder.rpm.spec", "MANIFEST*", "Makefile")
    	val outputDir = File("${project.buildDir}/rpm")
	outputs.dir(outputDir)
	outputs.file("cb-event-forwarder")
	dependsOn("getDeps")
	dependsOn("compileProtobufs")
	dependsOn("runUnitTests")
	doLast {
		project.delete(outputDir)
		project.exec {
			environment("RPM_OUTPUT_DIR" , outputDir)
			environment("GOPATH" , goPath)
			commandLine = listOf("make", "rpm")
		}
	}
}

val depTask = tasks.register<Exec>("getDeps") {
	inputs.dir("cmd/cb-event-forwarder")
	val gomodPath = "$goPath/pkg/mod"
	val gomodFile = File(gomodPath)
	if (! gomodFile.exists()) {
	     gomodFile.mkdirs()
	}
	inputs.dir("cmd/cb-event-forwarder")
	inputs.files("go.mod", "go.sum")
	outputs.upToDateWhen {
		val exitValue = project.exec {
			environment("GOPATH", goPath)
			commandLine = listOf("go","mod", "verify")
		}.getExitValue()
		exitValue == 0
	}
	inputs.files("go.sum", "go.mod")
	executable("make")
	environment("GOPATH", goPath)
	args("getdeps")
}

val protoGenerationTask = tasks.register<Exec>("compileProtobufs") {
	inputs.files("cmd/cb-event-forwarder/sensor_events.proto")
	outputs.files("cmd/cb-event-forwarder/sensor_events.pb.go")
	environment("GOPATH", goPath)
	executable("make")
	args("compile-protobufs")
}

val unitTestTask = tasks.register<Exec>("runUnitTests") {
	dependsOn("compileProtobufs")
	inputs.dir("cmd/cb-event-forwarder")
	inputs.dir("test/raw_data")
	val unitTestResultsFileName = "$buildDir/unittest.out"
	val unitTestResultsFile = File(unitTestResultsFileName)
	if (!unitTestResultsFile.exists()) {
		File("$buildDir").mkdirs()
		unitTestResultsFile.createNewFile()
	}
	val unitTestResultsFileStream = unitTestResultsFile.outputStream()
	executable("go")
       	args("test", "./cmd/cb-event-forwarder")
	environment("GOPATH", goPath)
	standardOutput =  unitTestResultsFileStream
	outputs.upToDateWhen {
		unitTestResultsFile.exists()
	}
	doLast { 
		unitTestResultsFile.inputStream().copyTo(System.out)
	}
}

val criticTask = tasks.register<Exec>("criticizeCode") {
	executable("make")
	args("critic")
}
