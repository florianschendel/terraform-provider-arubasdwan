# Read all appliances with their full deployment:
# hostname, serial number, and all configured IP interfaces
# (mgmt0/mgmt1, WAN, LAN, VLAN sub-interfaces, loopbacks).
data "arubasdwan_appliance_deployments" "all" {}

# Read a single appliance by its Orchestrator primary key.
data "arubasdwan_appliance_deployments" "branch01" {
  ne_pk = "3.NE"
}

# Server-side filtering (applied before any per-appliance data is
# fetched): exclude virtual appliances and lab systems, restrict to
# two sites. All criteria are combined with AND.
data "arubasdwan_appliance_deployments" "filtered" {
  exclude_models         = ["EC-V"]
  exclude_hostname_regex = "(?i)^(lab|test)-"
  sites                  = ["Berlin", "Hamburg"]
}

# Compact inventory overview: hostname -> serial + interface CIDRs.
output "inventory" {
  value = {
    for a in data.arubasdwan_appliance_deployments.all.appliances :
    a.hostname => {
      serial = a.serial
      # One entry per IP address: a dual-stack interface reports its name
      # once per address family, so group the CIDRs by name with "..."
      interfaces = { for i in a.interfaces : i.name => i.cidr... }
    }
  }
}

# WAN interfaces with private addresses: show the public IP
# discovered by the Orchestrator instead.
output "wan_public_ips" {
  value = {
    for a in data.arubasdwan_appliance_deployments.all.appliances :
    a.hostname => [
      for i in a.interfaces : {
        interface     = i.name
        configured_ip = i.cidr
        public_ip     = i.public_ip
      } if i.type == "wan" && i.is_private && i.public_ip != ""
    ]
  }
}

# Region, license, applied template groups, Business Intent Overlays
# (BIO), and static routes per appliance.
output "applied_config" {
  value = {
    for a in data.arubasdwan_appliance_deployments.all.appliances :
    a.hostname => {
      region          = a.region_name
      license_tier    = a.license.tier
      boost           = a.license.boost
      system_bw_kbps  = a.system_bandwidth_outbound
      template_groups = a.template_groups
      overlays        = [for o in a.overlays : o.name]
      static_routes = [
        for r in a.static_routes : {
          prefix   = r.prefix
          next_hop = r.next_hop
          vrf      = r.vrf_name
          metric   = r.metric
        }
      ]
    }
  }
}

# DHCP server/relay configuration of all LAN interfaces where enabled.
output "dhcp" {
  value = {
    for a in data.arubasdwan_appliance_deployments.all.appliances :
    a.hostname => {
      for i in a.interfaces : i.name => i.dhcp_config
      if i.dhcp_config != null
    }
  }
}

# Deployment settings of the WAN interfaces: interface label,
# VRF segment, security zone, firewall mode, and bandwidth limits.
output "wan_details" {
  value = {
    for a in data.arubasdwan_appliance_deployments.all.appliances :
    a.hostname => [
      for i in a.interfaces : {
        interface     = i.name
        label         = i.label
        vrf           = i.vrf_name
        zone          = i.zone_name
        firewall_mode = i.firewall_mode
        bw_out_kbps   = i.max_bandwidth_outbound
        bw_in_kbps    = i.max_bandwidth_inbound
      } if i.type == "wan"
    ]
  }
}
