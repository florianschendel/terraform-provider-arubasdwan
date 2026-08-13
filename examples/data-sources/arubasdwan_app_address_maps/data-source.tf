# Read all user-defined address maps.
data "arubasdwan_app_address_maps" "all" {}

# Name -> address or range.
output "address_maps" {
  value = {
    for m in data.arubasdwan_app_address_maps.all.address_maps :
    m.name => m.ip_start == m.ip_end ? m.ip_start : "${m.ip_start}-${m.ip_end}"
  }
}

# Which ranges belong to a given organization?
output "partner_ranges" {
  value = [
    for m in data.arubasdwan_app_address_maps.all.address_maps : m.name
    if m.org == "Partner Inc"
  ]
}
