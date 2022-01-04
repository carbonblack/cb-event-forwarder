import com.bmuschko.gradle.docker.tasks.container.*
import com.bmuschko.gradle.docker.tasks.image.*
import org.gradle.api.GradleException

plugins {
    base
    id("com.bmuschko.docker-remote-api")
}

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

val createDockerFile = tasks.register<Dockerfile>("createRegressionTestDockerfile") {
    from(System.getenv()["BASE_IMAGE"])
    runCommand("curl -O https://dlcdn.apache.org/kafka/3.0.0/kafka_2.13-3.0.0.tgz")
    runCommand("mkdir kafka && tar xf kafka_2.13-3.0.0.tgz -C kafka --strip-components 1")
}

val createRegressionTestImage = tasks.register<DockerBuildImage>("createRegressionTestImage") {
    dependsOn(createDockerFile)
    images.add("forwarderregressiontest/${osVersionClassifier}:latest")
}

val username: String = System.getProperties()["user.name"].toString()

val createContainer = tasks.register<DockerCreateContainer>("createContainer") {
    dependsOn(createRegressionTestImage)
    finalizedBy(":regressiontest:removeContainer")
    group = ""

    val imageid = createRegressionTestImage.get().imageId
    imageId.set(imageid)
    cmd.set(listOf("${projectDir}/cmd.sh", File("${rootProject.buildDir}/rpm").absolutePath, "${rootProject.projectDir.absolutePath}/regressiontest", "${rootProject.projectDir.absolutePath}/test/stress_rabbit/zipbundles/bundleone"))
    hostConfig.binds.set(mapOf((project.rootDir.absolutePath) to project.rootDir.absolutePath))
}

val startContainer = tasks.register<DockerStartContainer>("startContainer") {
    dependsOn(":build")
    dependsOn(createContainer)
    finalizedBy(":regressiontest:removeContainer")
    group = ""

    containerId.set(createContainer.get().containerId)
}

val tailContainer = tasks.register<DockerLogsContainer>("tailContainer") {
    dependsOn(startContainer)
    finalizedBy(":regressiontest:removeContainer")
    group = ""

    follow.set(true)
    containerId.set(createContainer.get().containerId)
}

val checkStatusCode = tasks.register<DockerWaitContainer>("checkStatusCode") {
    dependsOn(tailContainer)
    finalizedBy(":regressiontest:removeContainer")
    group = ""

    containerId.set(createContainer.get().containerId)

    doLast {
        if (exitCode != 0) {
            println("Smoke tests failed")
            throw GradleException("error occurred")
        }
    }
}

val removeContainer = tasks.register<DockerRemoveContainer>("removeContainer") {
    group = ""
    onlyIf {
        createContainer.get().state.failure != null ||
                startContainer.get().state.failure != null ||
                tailContainer.get().state.failure != null ||
                checkStatusCode.get().didWork
    }
    removeVolumes.set(true)
    force.set(true)
    containerId.set(createContainer.get().containerId)

    doFirst {
        println("Deleting created regressiontest container")
        onError {
            // ignore exception if container does not exist otherwise throw it
            if (!this.message!!.contains("No such container"))
                throw this
        }
    }
}

val regressiontest = tasks.register<Task>("runregressiontest") {
    dependsOn(checkStatusCode)
    group = "Verification"
    description = "Executes the smoke test suite."
}
