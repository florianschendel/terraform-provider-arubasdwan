resource "arubasdwan_app_dns_classification" "example" {
  name       = "Office365"
  domain     = "*.office365.com"
  confidence = 100
}
