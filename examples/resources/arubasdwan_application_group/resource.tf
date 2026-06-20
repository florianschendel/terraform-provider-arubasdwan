resource "arubasdwan_application_group" "example" {
  name = "InfraServices"
  apps = ["HTTP", "HTTPS"]
}
