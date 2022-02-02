pluginManagement {
    repositories {
        maven {
            url = uri("https://${System.getenv()["ARTIFACTORY_SERVER"] ?: "artifactory-pub.bit9.local"}:443/artifactory/java-all-release-virtual")
        }
    }
}

rootProject.name = "event-forwarder"

include(":regressiontest")
include(":smoketest")
include(":docker")
