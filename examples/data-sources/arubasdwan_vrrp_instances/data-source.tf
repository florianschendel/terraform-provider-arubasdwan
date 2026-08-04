# Read the VRRP instances of all appliances
data "arubasdwan_vrrp_instances" "all" {}

# VRRP overview per appliance: interface, group, virtual IP, and state
output "vrrp" {
  value = {
    for a in data.arubasdwan_vrrp_instances.all.appliances :
    a.hostname => [
      for v in a.vrrp_instances : {
        interface  = v.interface
        group_id   = v.group_id
        virtual_ip = v.virtual_ip
        priority   = v.priority
        state      = v.state
      }
    ] if length(a.vrrp_instances) > 0
  }
}

# All virtual IP addresses in use, e.g. for documentation exports (FHRP/VRRP
# groups in an IPAM/DCIM system such as NetBox)
output "virtual_ips" {
  value = distinct(flatten([
    for a in data.arubasdwan_vrrp_instances.all.appliances : [
      for v in a.vrrp_instances : v.virtual_ip
    ]
  ]))
}

# Read a single appliance by its Orchestrator primary key
data "arubasdwan_vrrp_instances" "branch01" {
  ne_pk = "3.NE"
}

# Server-side filtering: exclude lab systems, restrict to one site.
# Filtered appliances are never queried.
data "arubasdwan_vrrp_instances" "filtered" {
  exclude_hostname_regex = "(?i)^(lab|test)-"
  sites                  = ["Berlin"]
}
