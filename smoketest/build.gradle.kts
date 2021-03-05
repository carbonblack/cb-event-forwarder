import com.bmuschko.gradle.docker.tasks.container.*
import org.gradle.api.GradleException

plugins {
    base
    id("com.bmuschko.docker-remote-api")
}

val username: String = System.getProperties()["user.name"].toString()

val createContainer = tasks.register<DockerCreateContainer>("createContainer") {
    finalizedBy(":smoketest:removeContainer")
    group = ""

    imageId.set(System.getenv()["BASE_IMAGE"])
    cmd.set(listOf("${projectDir}/cmd.sh", File("${rootProject.buildDir}/rpm").absolutePath, "${rootProject.projectDir.absolutePath}/smoketest/cb-event-forwarder.docker.ini", "${rootProject.projectDir.absolutePath}/test/stress_rabbit/zipbundles/bundleone"))
    hostConfig.binds.set(mapOf((project.rootDir.absolutePath) to project.rootDir.absolutePath))
}

val startContainer = tasks.register<DockerStartContainer>("startContainer") {
    dependsOn(":build")
    dependsOn(createContainer)
    finalizedBy(":smoketest:removeContainer")
    group = ""

    containerId.set(createContainer.get().containerId)
}

val tailContainer = tasks.register<DockerLogsContainer>("tailContainer") {
    dependsOn(startContainer)
    finalizedBy(":smoketest:removeContainer")
    group = ""

    follow.set(true)
    containerId.set(createContainer.get().containerId)
}

val checkStatusCode = tasks.register<DockerWaitContainer>("checkStatusCode") {
    dependsOn(tailContainer)
    finalizedBy(":smoketest:removeContainer")
    group = ""

    containerId.set(createContainer.get().containerId)

    doLast {
        if(exitCode != 0) {
            println("Smoke tests failed")
            throw GradleException("error occurred")
        }
    }
}

val removeContainer = tasks.register<DockerRemoveContainer>("removeContainer") {
    group = ""
    onlyIf { createContainer.get().state.failure != null ||
             startContainer.get().state.failure != null ||
             tailContainer.get().state.failure != null ||
             checkStatusCode.get().didWork }
    removeVolumes.set(true)
    force.set(true)
    containerId.set(createContainer.get().containerId)

    doFirst{
        println("Deleting created smoketest container")
        onError {
            // ignore exception if container does not exist otherwise throw it
            if (!this.message!!.contains("No such container"))
                throw this
        }
    }
}

val smoketest = tasks.register<Task>("runSmokeTest") {
    dependsOn(checkStatusCode)
    group = "Verification"
    description = "Executes the smoke test suite."
}

tasks.named("build") {
   this.finalizedBy(smoketest)
}
