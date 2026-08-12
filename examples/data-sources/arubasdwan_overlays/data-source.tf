# Read all Business Intent Overlays (BIO) with their traffic matching.
data "arubasdwan_overlays" "all" {}

# Overview: how each overlay selects traffic.
output "overlay_matching" {
  value = {
    for o in data.arubasdwan_overlays.all.overlays : o.name => {
      id         = o.id
      match_type = o.match_type
      acl        = o.acl_name
      entries = [
        for e in o.acl_entries :
        "${e.sequence}: ${e.permit ? "permit" : "deny"} ${e.match_all ? "all" : coalesce(e.application, e.app_group, "")}"
      ]
    }
  }
}

# Which overlays carry a given application group?
output "overlays_with_critical_apps" {
  value = [
    for o in data.arubasdwan_overlays.all.overlays : o.name
    if length([for e in o.acl_entries : e if e.app_group == "CriticalApps"]) > 0
  ]
}

# Raw ACL of one overlay, exactly as stored by the Orchestrator.
output "business_acl_raw" {
  value = one([
    for o in data.arubasdwan_overlays.all.overlays : o.acl_raw if o.name == "Business"
  ])
}
