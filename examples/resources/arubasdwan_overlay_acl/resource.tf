# An application group managed by this provider.
resource "arubasdwan_application_group" "critical" {
  name = "CriticalApps"
  apps = ["salesforce", "office365"]
}

# A custom DNS-based application.
resource "arubasdwan_app_dns_classification" "internal_crm" {
  name   = "internal-crm"
  domain = "crm.example.com"
}

# Attach both to the "Business" overlay, plus an explicit deny.
# The overlay must already exist; only its match configuration is replaced.
resource "arubasdwan_overlay_acl" "business" {
  overlay_name = "Business"

  entries = [
    {
      sequence  = 10
      app_group = arubasdwan_application_group.critical.name
      comment   = "business critical applications"
    },
    {
      sequence    = 20
      application = arubasdwan_app_dns_classification.internal_crm.name
    },
    {
      sequence    = 30
      application = "bittorrent"
      permit      = false
      comment     = "never carry P2P in this overlay"
    },
  ]
}

# A catch-all overlay: named applications first, everything else last.
resource "arubasdwan_overlay_acl" "internet" {
  overlay_name = "Internet"

  entries = [
    {
      sequence  = 10
      app_group = "SaaS"
    },
    {
      sequence  = 999
      match_all = true
      comment   = "everything else"
    },
  ]
}

# Removing entries that exist on the Orchestrator but not in the
# configuration — for example rules somebody added in the UI — aborts plan
# and apply. Confirm such removals with allow_entry_removal on the resource
# (not on an individual entry). Treat it as a temporary switch: set it,
# apply, then remove it again so the guard stays active.
resource "arubasdwan_overlay_acl" "default_overlay_cleanup" {
  overlay_name        = "DefaultOverlay"
  allow_entry_removal = true

  entries = [
    {
      sequence  = 1900
      app_group = "CriticalApps"
      comment   = "managed by Terraform"
    },
    {
      sequence  = 1905
      match_all = true
    },
  ]
}
