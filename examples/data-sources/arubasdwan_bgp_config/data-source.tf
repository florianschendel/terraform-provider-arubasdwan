# Read the BGP configuration of all appliances
data "arubasdwan_bgp_config" "all" {}

# BGP overview per appliance: local ASN, router ID, and peers per VRF
output "bgp" {
  value = {
    for a in data.arubasdwan_bgp_config.all.appliances :
    a.hostname => [
      for v in a.vrfs : {
        vrf       = v.vrf_name
        asn       = v.system.asn
        router_id = v.system.router_id
        enabled   = v.system.enabled
        peers = [
          for n in v.neighbors : {
            ip        = n.ip
            remote_as = n.remote_as
            type      = n.type
          }
        ]
      }
    ] if length(a.vrfs) > 0
  }
}

# All remote AS numbers peered with, e.g. for documentation exports
output "remote_as_numbers" {
  value = distinct(flatten([
    for a in data.arubasdwan_bgp_config.all.appliances : [
      for v in a.vrfs : [
        for n in v.neighbors : n.remote_as
      ]
    ]
  ]))
}

# Read a single appliance by its Orchestrator primary key
data "arubasdwan_bgp_config" "branch01" {
  ne_pk = "3.NE"
}

# Server-side filtering: exclude lab systems, restrict to one site.
# Filtered appliances are never queried.
data "arubasdwan_bgp_config" "filtered" {
  exclude_hostname_regex = "(?i)^(lab|test)-"
  sites                  = ["Berlin"]
}
