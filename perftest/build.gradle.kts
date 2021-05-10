import com.bmuschko.gradle.docker.tasks.container.*
import com.bmuschko.gradle.docker.tasks.image.*
import org.gradle.api.GradleException
import java.io.BufferedReader


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

fun List<String>.execute(workingDir: File? = null): String? {
    val proc = ProcessBuilder(this)
            .directory(workingDir)
            .redirectOutput(ProcessBuilder.Redirect.PIPE)
            .redirectError(ProcessBuilder.Redirect.PIPE)
            .start()

    val allText = proc.inputStream.bufferedReader().use(BufferedReader::readText)
    proc.waitFor(5, TimeUnit.SECONDS)
    return allText
}

buildDir = rootProject.buildDir

val createDockerFile = tasks.register<Dockerfile>("createperftestDockerfile") {
    from(System.getenv()["BASE_IMAGE"])
    runCommand("wget https://dl.min.io/server/minio/release/linux-amd64/minio && chmod +x minio")
}

val createperftestImage = tasks.register<DockerBuildImage>("createperftestImage") {
    dependsOn(createDockerFile)
    images.add("eventforwarderperftest/${osVersionClassifier}:latest")
}

val username: String = System.getProperties()["user.name"].toString()

val createContainer = tasks.register<DockerCreateContainer>("createContainer") {
    dependsOn(createperftestImage)
    finalizedBy(":perftest:removeContainer")
    group = ""

    imageId.set(createperftestImage.get().imageId)
    cmd.set(listOf("${projectDir}/cmd.sh", File("${rootProject.buildDir}/rpm").absolutePath, "${rootProject.projectDir.absolutePath}/perftest/", "${rootProject.projectDir.absolutePath}/test/stress_rabbit/zipbundles/bundleone"))
    hostConfig.binds.set(mapOf((project.rootDir.absolutePath) to project.rootDir.absolutePath))
}

val startContainer = tasks.register<DockerStartContainer>("startContainer") {
    dependsOn(":build")
    dependsOn(createContainer)
    finalizedBy(":perftest:removeContainer")
    group = ""

    containerId.set(createContainer.get().containerId)
}

val tailContainer = tasks.register<DockerLogsContainer>("tailContainer") {
    dependsOn(startContainer)
    finalizedBy(":perftest:removeContainer")
    group = ""

    follow.set(true)
    containerId.set(createContainer.get().containerId)
}

val checkStatusCode = tasks.register<DockerWaitContainer>("checkStatusCode") {
    dependsOn(tailContainer)
    finalizedBy(":perftest:removeContainer")
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
        println("Deleting created perftest container")
        onError {
            // ignore exception if container does not exist otherwise throw it
            if (!this.message!!.contains("No such container"))
                throw this
        }
    }
}

val perftest = tasks.register<Task>("runPerfTest") {
    dependsOn(checkStatusCode)
    group = "Verification"
    description = "Executes the perf test suite."
}
