terraform {
  required_providers {
    arubasdwan = {
      source = "florianschendel/arubasdwan"
    }
  }
}

provider "arubasdwan" {
  orchestrator_url = var.orch_uri
  api_key          = var.orch_api_key
  insecure         = true
}

variable "orch_api_key" {
  description = "Orchestrator api key"
}

variable "orch_uri" {
  description = "Orchestrator URL"
}

# =============================================================================
# VRF Segments
# =============================================================================

data "arubasdwan_vrf_segments" "default" {
  source_vrf = "Default"
  dest_vrf   = "Default"
}

output "all_vrf_segments" {
  value = data.arubasdwan_vrf_segments.default.segments
}

output "default_segment_pair" {
  value = data.arubasdwan_vrf_segments.default.segment_pair
}

output "zone_mappings" {
  value = data.arubasdwan_vrf_segments.default.zone_mappings
}

# =============================================================================
# Security Zones
# =============================================================================

data "arubasdwan_security_zones" "all" {}

output "all_security_zones" {
  value = data.arubasdwan_security_zones.all.security_zones
}

resource "arubasdwan_security_zone" "production" {
  name = "Production"
}

resource "arubasdwan_security_zone" "development" {
  name = "Development"
}

resource "arubasdwan_security_zone" "lab" {
  name = "Lab"
}

# =============================================================================
# Port/Protocol Classifications
# =============================================================================

data "arubasdwan_app_port_protocols" "all" {}

output "all_app_port_protocols" {
  value = data.arubasdwan_app_port_protocols.all.port_protocol_classifications
}

resource "arubasdwan_app_port_protocol" "internal_api" {
  name        = "InternalAPI"
  port        = 8443
  protocol    = 6 # TCP
  description = "Internal API service"
  confidence  = 50
}

resource "arubasdwan_app_port_protocol" "monitoring" {
  name        = "Monitoring"
  port        = 9090
  protocol    = 6 # TCP
  description = "Prometheus monitoring"
}

# =============================================================================
# DNS Classifications
# =============================================================================

data "arubasdwan_app_dns_classifications" "all" {}

output "all_dns_classifications" {
  value = data.arubasdwan_app_dns_classifications.all.dns_classifications
}

resource "arubasdwan_app_dns_classification" "office365" {
  name       = "Office365"
  domain     = "*.office365.com"
  confidence = 100
}

resource "arubasdwan_app_dns_classification" "salesforce" {
  name        = "Salesforce"
  domain      = "*.salesforce.com"
  confidence  = 80
  description = "Salesforce CRM"
}

# =============================================================================
# Compound Classifications
# =============================================================================

data "arubasdwan_app_compound_classifications" "all" {}

output "all_compound_classifications" {
  value = data.arubasdwan_app_compound_classifications.all.compound_classifications
}

resource "arubasdwan_app_compound_classification" "internal_api_traffic" {
  name        = "InternalAPITraffic"
  description = "Internal API traffic"
  protocol    = "tcp"
  dst_ip      = "10.0.0.0/8"
  dst_port    = "443,8443"
}

resource "arubasdwan_app_compound_classification" "teams_udp" {
  name        = "MSTeamsUDP"
  description = "Microsoft Teams UDP traffic"
  protocol    = "udp"
  dst_port    = "3478-3481"
  dst_ip      = "13.107.64.0/18,52.112.0.0/14"
}

# =============================================================================
# Application Search (built-in + user-defined apps)
# =============================================================================

data "arubasdwan_app_search" "skype" {
  pattern = "skype"
  limit   = 0
}

output "skype_apps" {
  value = data.arubasdwan_app_search.skype.applications
}

# =============================================================================
# Application Groups
# =============================================================================

data "arubasdwan_application_groups" "all" {}

output "all_application_groups" {
  value = data.arubasdwan_application_groups.all.application_groups
}

resource "arubasdwan_application_group" "infra_services" {
  name = "InfraServices"
  apps = [
    arubasdwan_app_port_protocol.internal_api.name,
    arubasdwan_app_port_protocol.monitoring.name,
  ]
}

resource "arubasdwan_application_group" "web_apps" {
  name = "WebApps"
  apps = [
    "HTTP",
    "HTTPS",
    arubasdwan_app_port_protocol.internal_api.name,
  ]
}

# =============================================================================
# IP Address Groups
# =============================================================================

data "arubasdwan_ip_address_groups" "all" {}

output "all_ip_address_groups" {
  value = data.arubasdwan_ip_address_groups.all.address_groups
}

resource "arubasdwan_ip_address_group" "office_subnets" {
  name = "OfficeSubnets"
  rules = [
    {
      included_ips    = ["192.168.1.0/24", "192.168.2.0/24"]
      excluded_ips    = ["192.168.1.5/32"]
      included_groups = []
      comment         = "Main office subnets"
    },
    {
      included_ips    = ["10.10.0.0/16"]
      excluded_ips    = []
      included_groups = []
      comment         = "Branch office"
    },
  ]
}

# =============================================================================
# Security Policies
# =============================================================================

data "arubasdwan_security_policies" "default" {
  segment_pair = data.arubasdwan_vrf_segments.default.segment_pair
}

output "all_security_policies" {
  value = data.arubasdwan_security_policies.default.security_policies
}

# Block traffic by geo location (highest priority)
resource "arubasdwan_security_policy" "geo_block" {
  segment_pair   = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 20000
  action         = "deny"
  dst_geo        = "CN"
  comment        = "Block traffic to CN"
}

# Allow infrastructure services from Production to Development
resource "arubasdwan_security_policy" "allow_infra" {
  segment_pair   = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 30000
  action         = "allow"
  app_group      = arubasdwan_application_group.infra_services.name
  comment        = "Allow infrastructure services"
}

# Allow web traffic from Lab to Production
resource "arubasdwan_security_policy" "allow_web" {
  segment_pair   = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id = arubasdwan_security_zone.lab.id
  dest_zone_id   = arubasdwan_security_zone.production.id
  priority       = 30000
  action         = "allow"
  app_group      = arubasdwan_application_group.web_apps.name
  comment        = "Allow web apps from lab"
}

# Allow specific IP range with protocol filter
resource "arubasdwan_security_policy" "allow_subnet" {
  segment_pair   = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 35000
  action         = "allow"
  src_ip         = "192.168.1.0/24"
  dst_ip         = "10.0.0.0/8"
  dst_port       = "443"
  protocol       = "tcp"
  comment        = "Allow HTTPS from office subnet"
}

# Allow traffic from an IP address group (referenced by name)
resource "arubasdwan_security_policy" "allow_from_office_subnets" {
  segment_pair      = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id    = arubasdwan_security_zone.production.id
  dest_zone_id      = arubasdwan_security_zone.development.id
  priority          = 27000
  action            = "allow"
  protocol          = "tcp"
  dst_port          = "22"
  dst_ip            = "10.0.0.0/8"
  src_address_group = arubasdwan_ip_address_group.office_subnets.name
  comment           = "SSH from office subnets"
}

# Default deny rule
resource "arubasdwan_security_policy" "default_deny_prod_dev" {
  segment_pair   = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 65535
  action         = "deny"
  comment        = "Default deny"
}
