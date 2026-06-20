resource "arubasdwan_security_policy" "example" {
  segment_pair   = "0:0"
  source_zone_id = 1
  dest_zone_id   = 2
  priority       = 30000
  action         = "allow"
  app_group      = "WebApps"
  comment        = "Allow web apps"
}
