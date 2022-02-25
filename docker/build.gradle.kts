import com.bmuschko.gradle.docker.tasks.container.*
import com.bmuschko.gradle.docker.tasks.image.*
import org.gradle.api.GradleException
import java.io.BufferedReader
import java.io.ByteArrayOutputStream


plugins {
    base
    id("com.bmuschko.docker-remote-api")
}

val osVersionClassifier: String
    get() {
        return try {
            val versionText = File("/etc/redhat-release").readText()
            when {
                versionText.contains("release 8") -> "centos8"
                else -> "centos7"
            }
        } catch (ignored: Exception) {
            "centos7"
        }
    }

val createProdDockerFile = tasks.register("createProdDockerfile") {
    dependsOn(":build")
    doLast { 
        val rpmDir = "${rootProject.buildDir}/rpm"
        val findCommand = "find \"$rpmDir\" -name \"*.rpm\" -print -quit"
        val output = ByteArrayOutputStream()
        project.exec {
            commandLine = listOf("bash", "-c", findCommand)
            standardOutput = output
        }
        val rpmFile = File(output.toString().trim())
        output.close()
        val destinationFile = File("docker/build/docker/${rpmFile.name}")
        if (destinationFile.exists()) {
            destinationFile.delete()
        }
        rpmFile.copyTo(destinationFile)
        val dockerFile = File("docker/Dockerfile")
        val destinationDockerFile = File("docker/build/docker/Dockerfile")
        if (destinationDockerFile.exists()) {
            destinationDockerFile.delete()
        }
        dockerFile.copyTo(destinationDockerFile)

    }
}

val createProdImage = tasks.register<DockerBuildImage>("createProdImage") {
    dependsOn(createProdDockerFile)
    images.add("eventforwarder/${osVersionClassifier}:latest")
}

val createProdTestContainer = tasks.register<DockerCreateContainer>("createProdTestContainer") {
    dependsOn(createProdImage)
    finalizedBy(":docker:removeProdTestContainer")
    group = ""

    imageId.set(createProdImage.get().imageId)
    cmd.set(listOf("${projectDir}/cmd.sh", File("${rootProject.buildDir}/rpm").absolutePath, "${rootProject.projectDir.absolutePath}/smoketest/cb-event-forwarder.docker.ini", "${rootProject.projectDir.absolutePath}/test/stress_rabbit/zipbundles/bundleone"))
    hostConfig.binds.set(mapOf((project.rootDir.absolutePath) to project.rootDir.absolutePath))

}

val startProdTestContainer = tasks.register<DockerStartContainer>("startProdTestContainer") {
    dependsOn(createProdTestContainer)
    finalizedBy(":docker:removeProdTestContainer")
    group = ""

    containerId.set(createProdTestContainer.get().containerId)
}

val tailProdTestContainer = tasks.register<DockerLogsContainer>("tailProdTestContainer") {
    dependsOn(startProdTestContainer)
    finalizedBy(":docker:removeProdTestContainer")
    group = ""

    follow.set(true)
    containerId.set(createProdTestContainer.get().containerId)
}

val checkProdTestStatusCode = tasks.register<DockerWaitContainer>("checkProdTestStatusCode") {
    dependsOn(tailProdTestContainer)
    finalizedBy(":docker:removeProdTestContainer")
    group = ""

    containerId.set(createProdTestContainer.get().containerId)

    doLast {
        if (exitCode != 0) {
            println("Prod tests failed")
            throw GradleException("error occurred")
        }
    }
}

val removeProdTestContainer = tasks.register<DockerRemoveContainer>("removeProdTestContainer") {
    group = ""
    onlyIf {
        createProdTestContainer.get().state.failure != null ||
                startProdTestContainer.get().state.failure != null ||
                tailProdTestContainer.get().state.failure != null ||
                checkProdTestStatusCode.get().didWork
    }
    removeVolumes.set(true)
    force.set(true)
    containerId.set(createProdTestContainer.get().containerId)

    doFirst {
        println("Deleting created ProdTest container")
        onError {
            // ignore exception if container does not exist otherwise throw it
            if (!this.message!!.contains("No such container"))
                throw this
        }
    }
}

val dockerProdTest = tasks.register<Task>("dockerTest") {
    dependsOn(checkProdTestStatusCode)
    group = "Verification"
    description = "Executes the prod test docker container build and tests"
}

val dockerBuild = tasks.register<Task>("buildDocker") {
    dependsOn(dockerProdTest)
    group = "Verification"
    description = "build prod docker image"
}
