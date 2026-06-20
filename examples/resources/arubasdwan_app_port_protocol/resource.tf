resource "arubasdwan_app_port_protocol" "example" {
  name        = "InternalAPI"
  port        = 8443
  protocol    = 6
  description = "Internal API service"
  confidence  = 50
}
