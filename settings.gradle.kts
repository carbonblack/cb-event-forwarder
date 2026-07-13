pluginManagement {
    repositories {
        maven {
            url = uri(System.getenv("ARTIFACTORY_URL") ?: "")
            credentials{
                username = System.getenv("ACCESS_ID")
                password = System.getenv("ACCESS_TOKEN")
            }
        }
    }
}

rootProject.name = "event-forwarder"

include(":regressiontest")
include(":smoketest")
include(":docker")
