# An address map classifies an IPv4 range as a named application. Set ip_end
# to the same value as ip_start to classify a single host.
resource "arubasdwan_app_address_map" "stream_server" {
  name         = "stream-server"
  ip_start     = "10.0.13.72"
  ip_end       = "10.0.13.72"
  description  = "Streaming server, on-premises"
  country      = "Germany"
  country_code = "DE"
  org          = "Example Org"
}

# A contiguous range, with a higher priority than the default of 100.
resource "arubasdwan_app_address_map" "partner_range" {
  name     = "partner-api"
  ip_start = "203.0.113.10"
  ip_end   = "203.0.113.19"
  org      = "Partner Inc"
  priority = 200
}

# Policies and overlay ACLs reference the classification by name.
resource "arubasdwan_overlay_acl" "business" {
  overlay_name = "Business"

  entries = [
    {
      sequence       = 1000
      either_service = arubasdwan_app_address_map.stream_server.name
      comment        = "on-premises streaming"
    },
  ]
}
