pluginManagement {
    repositories {
        maven {
            url = uri("https://cb-gradle-dev-virtual.usw1.packages.broadcom.com/cb-gradle-dev-virtual")
            credentials{
                username = System.getenv("USW1_ACCESS_ID_DEV")
                password = System.getenv("USW1_ACCESS_TOKEN_DEV")
            }
        }
        maven {
            url = uri("https://cb-gradle-dev-virtual.usw1.packages.broadcom.com/cb-maven-prod-virtual")
            credentials{
                username = System.getenv("USW1_ACCESS_ID_DEV")
                password = System.getenv("USW1_ACCESS_TOKEN_DEV")
            }
        }
    }
}

rootProject.name = "event-forwarder"

include(":regressiontest")
include(":smoketest")
include(":docker")
include(":blackduck")
