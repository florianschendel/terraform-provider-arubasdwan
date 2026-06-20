resource "arubasdwan_app_compound_classification" "example" {
  name        = "InternalAPITraffic"
  description = "Internal API traffic"
  protocol    = "tcp"
  dst_ip      = "10.0.0.0/8"
  dst_port    = "443,8443"
}
