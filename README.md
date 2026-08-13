# Terraform Provider for Aruba EdgeConnect SD-WAN Orchestrator

A Terraform provider for managing resources on the **Aruba EdgeConnect SD-WAN Orchestrator 9.6**, built using the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework).

## Aruba Orchestrator API documentation
https://developer.arubanetworks.com/edgeconnect/docs/rest-api-table-for-93
https://developer.arubanetworks.com/edgeconnect/reference/


## Features

- **API Key Authentication** against the Orchestrator REST API
- **Security Zones** — read and manage firewall security zones
  - `arubasdwan_security_zone` (resource): Create, read, update, and delete security zones
  - `arubasdwan_security_zones` (data source): List all existing security zones
- **Security Policies** — read and manage firewall security policies
  - `arubasdwan_security_policy` (resource): Create, read, update, and delete security policy rules. Supports references to `arubasdwan_ip_address_group`, `arubasdwan_application_group`, and `arubasdwan_app_*_classification` resources. Multi-value IP/port fields accept comma-separated lists in HCL.
  - `arubasdwan_security_policies` (data source): List all policies for a segment pair
- **Application Definitions** — define custom applications by various classification methods
  - `arubasdwan_app_port_protocol` (resource): Port/protocol-based applications
  - `arubasdwan_app_dns_classification` (resource): DNS/domain-based applications
  - `arubasdwan_app_dns_classifications` (data source): List all user-defined DNS classification applications
  - `arubasdwan_app_compound_classification` (resource): Compound match-based applications (IP, port, protocol, DNS, geo, service, DSCP)
  - `arubasdwan_app_compound_classifications` (data source): List all user-defined compound classification applications
  - `arubasdwan_app_port_protocols` (data source): List all user-defined port/protocol applications
  - `arubasdwan_app_address_map` (resource): Address maps — classify an IPv4 address range as a named application (API: IP intelligence classification)
  - `arubasdwan_app_address_maps` (data source): List all user-defined address maps
  - `arubasdwan_app_search` (data source): Wildcard search across **all** applications on the Orchestrator (built-in + user-defined)
- **Application Groups** — group applications for use in policies
  - `arubasdwan_application_group` (resource): Create, read, update, and delete application groups
  - `arubasdwan_application_groups` (data source): List all user-defined application groups
- **IP Objects** — reusable IP address/service groups managed centrally on the Orchestrator
  - `arubasdwan_ip_address_group` (resource): Create, read, update, and delete IP address groups
  - `arubasdwan_ip_address_groups` (data source): List all IP address groups
- **VRF Segments** — read VRF segments and resolve segment pairs by name
  - `arubasdwan_vrf_segments` (data source): List all VRF segments and optionally resolve a segment pair from VRF names
- **Business Intent Overlays (BIO)** — read overlays and attach applications/application groups to them
  - `arubasdwan_overlays` (data source): List all overlays with their traffic matching (interface label, appliance ACL, or built-in overlay ACL including its entries)
  - `arubasdwan_overlay_acl` (resource): Manage the ACL built into an overlay — attach applications (DNS, compound, port/protocol) and application groups, and match on IP, ports, protocol, DSCP, DNS, services, address groups, or VRF
- **Appliance Inventory** — read appliances and their deployment for documentation/export use cases
  - `arubasdwan_appliance_deployments` (data source): List all appliances with hostname, serial number, and every configured IP interface (mgmt, WAN, LAN, VLAN sub-interfaces, loopbacks) including interface label, VRF segment, security zone, firewall mode, bandwidth limits, and the public IP discovered by the Orchestrator for WAN interfaces behind NAT. Also reports the SD-WAN region, applied template groups, Business Intent Overlays (BIO), locally configured static routes, EC license, system bandwidth, and the DHCP server/relay configuration of LAN interfaces per appliance
- **VRRP** — read the VRRP configuration of the appliances for documentation/export use cases
  - `arubasdwan_vrrp_instances` (data source): List the VRRP instances configured on the appliances: group ID (VRID), peering interface, virtual IP address, priority, preemption, and timers, plus the operational state (Master/Backup/Init, current master IP, transitions, uptime, virtual MAC)
- **BGP** — read the BGP configuration of the appliances for documentation/export use cases
  - `arubasdwan_bgp_config` (data source): List the BGP configuration of the appliances per VRF segment: system settings (local ASN, router ID, graceful restart, OSPF redistribution) and configured neighbors (peer IP, remote ASN, peer type, timers, route policies, BFD)

## Requirements

- [Go](https://golang.org/doc/install) >= 1.21
- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0

## Building the Provider

From the repository root:

```bash
go build -o terraform-provider-arubasdwan
```

Or use the provided Makefile targets:

```bash
make build      # Build the binary in the repo root
make install    # Build and install into ~/.terraform.d/plugins/registry.terraform.io/example/arubasdwan/0.1.0/<os>_<arch>
make fmt        # go fmt ./...
make vet        # go vet ./...
```

## Local Development Setup

1. Build the provider binary:

   ```bash
   go build -o terraform-provider-arubasdwan
   ```

2. Create or edit `~/.terraformrc` to use the local build:

   ```hcl
   provider_installation {
     dev_overrides {
       "registry.terraform.io/example/arubasdwan" = "/path/to/terraform-provider-arubasdwan"
     }
     direct {}
   }
   ```

   Replace `/path/to/terraform-provider-arubasdwan` with the directory containing the compiled binary.

3. Run Terraform commands without `terraform init`:

   ```bash
   terraform plan
   terraform apply
   ```

## Provider Configuration

```hcl
terraform {
  required_providers {
    arubasdwan = {
      source = "registry.terraform.io/example/arubasdwan"
    }
  }
}

provider "arubasdwan" {
  orchestrator_url = "https://192.168.64.2"
  api_key          = var.orchestrator_api_key
  insecure         = true  # Skip TLS verification (for self-signed certificates)
}
```

### Provider Arguments

| Argument           | Type   | Required | Description                                                        |
|--------------------|--------|----------|--------------------------------------------------------------------|
| `orchestrator_url` | string | yes      | Base URL of the Aruba SD-WAN Orchestrator (e.g. `https://192.168.64.2`) |
| `api_key`          | string | yes      | API key for authenticating with the Orchestrator REST API          |
| `insecure`         | bool   | no       | Skip TLS certificate verification (default: `false`)              |

> **Security note:** Never hard-code your API key in `.tf` files. Use environment variables or a `terraform.tfvars` file (excluded from version control) instead.

---

## Resource: `arubasdwan_security_zone`

Manages a security zone on the Orchestrator.

### Arguments

| Argument | Type   | Required | Description                                            |
|----------|--------|----------|--------------------------------------------------------|
| `name`   | string | yes      | Name of the security zone                              |

### Attributes

| Attribute | Type  | Description                              |
|-----------|-------|------------------------------------------|
| `id`      | int64 | Unique identifier assigned by Orchestrator |

### Example

```hcl
resource "arubasdwan_security_zone" "production" {
  name = "Production"
}

resource "arubasdwan_security_zone" "development" {
  name = "Development"
}
```

### Import

```bash
terraform import arubasdwan_security_zone.production 42
```

---

## Data Source: `arubasdwan_security_zones`

Retrieves all security zones configured on the Orchestrator.

### Attributes

| Attribute        | Type | Description                     |
|------------------|------|---------------------------------|
| `security_zones` | list | List of security zone objects   |

Each object in `security_zones` contains:

| Field  | Type   | Description                              |
|--------|--------|------------------------------------------|
| `id`   | int64  | Unique identifier of the security zone   |
| `name` | string | Name of the security zone                |

### Example

```hcl
data "arubasdwan_security_zones" "all" {}

output "zones" {
  value = data.arubasdwan_security_zones.all.security_zones
}
```

---

## Resource: `arubasdwan_app_port_protocol`

Manages a user-defined application based on port/protocol classification on the Orchestrator. Application definitions can be referenced by name in security policies and application groups.

> Creating a port/protocol pair that already exists on the Orchestrator is rejected (already at plan time) (the API would silently overwrite it) — import it instead. Duplicate names across port/protocol classifications are rejected as well, since policies reference applications by name.

### Arguments

| Argument      | Type   | Required | Default | Description                                                      |
|---------------|--------|----------|---------|------------------------------------------------------------------|
| `name`        | string | yes      |         | Application name (alphanumeric, hyphens, underscores; max 31 chars) |
| `port`        | int64  | yes      |         | Port number (use 0 for IP protocol applications). **Forces replacement on change.** |
| `protocol`    | int64  | yes      |         | Protocol number (6 = TCP, 17 = UDP). **Forces replacement on change.** |
| `description` | string | no       | `""`    | Description of the application                                   |
| `confidence`  | int64  | no       | `50`    | Confidence level of the classification (0-100)                   |
| `disabled`    | bool   | no       | `false` | Whether the application definition is disabled                   |

### Attributes

| Attribute | Type   | Description                                   |
|-----------|--------|-----------------------------------------------|
| `id`      | string | Composite ID: `port_protocol` (e.g. `"8443_6"`) |

### Example

```hcl
# Define a custom HTTPS application on port 8443
resource "arubasdwan_app_port_protocol" "custom_https" {
  name        = "CustomHTTPS"
  port        = 8443
  protocol    = 6       # TCP
  description = "Custom HTTPS service"
  confidence  = 50
}

# Define a custom UDP application
resource "arubasdwan_app_port_protocol" "custom_udp_app" {
  name        = "MyUDPApp"
  port        = 5000
  protocol    = 17      # UDP
  description = "Custom UDP service"
}
```

### Import

Import using the `port_protocol` format:

```bash
terraform import arubasdwan_app_port_protocol.custom_https 8443_6
```

---

## Data Source: `arubasdwan_app_port_protocols`

Retrieves all user-defined application definitions from the Orchestrator.

### Attributes

| Attribute                | Type | Description                              |
|--------------------------|------|------------------------------------------|
| `port_protocol_classifications`| list | List of application definition objects   |

Each object contains:

| Field         | Type   | Description                    |
|---------------|--------|--------------------------------|
| `name`        | string | Application name               |
| `port`        | int64  | Port number                    |
| `protocol`    | int64  | Protocol number                |
| `description` | string | Description                    |
| `priority`    | int64  | Priority / confidence level    |
| `disabled`    | bool   | Whether the definition is disabled |

### Example

```hcl
data "arubasdwan_app_port_protocols" "all" {}

output "custom_apps" {
  value = data.arubasdwan_app_port_protocols.all.port_protocol_classifications
}
```

---

## Resource: `arubasdwan_app_dns_classification`

Manages a DNS/domain-based application definition. Applications are matched by domain name pattern.

> Creating a domain that already exists on the Orchestrator is rejected (already at plan time) (the API would silently overwrite it) — import it instead. Duplicate names across DNS classifications are rejected as well, since policies reference applications by name.

### Arguments

| Argument      | Type   | Required | Default | Description                                              |
|---------------|--------|----------|---------|----------------------------------------------------------|
| `name`        | string | yes      |         | Application name                                         |
| `domain`      | string | yes      |         | Domain pattern (e.g. `"*.example.com"`). **Forces replacement.** |
| `confidence`  | int64  | yes      |         | Confidence level of the classification (1-100)           |
| `description` | string | no       | `""`    | Description                                              |
| `disabled`    | bool   | no       | `false` | Whether disabled                                         |

### Example

```hcl
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
```

### Import

```bash
terraform import arubasdwan_app_dns_classification.office365 "*.office365.com"
```

---

## Data Source: `arubasdwan_app_dns_classifications`

Retrieves all user-defined DNS classification application definitions from the Orchestrator.

### Attributes

| Attribute             | Type | Description                                   |
|-----------------------|------|-----------------------------------------------|
| `dns_classifications` | list | List of DNS classification definition objects |

Each object contains:

| Field         | Type   | Description                                 |
|---------------|--------|---------------------------------------------|
| `name`        | string | Application name                            |
| `domain`      | string | DNS domain pattern (e.g. `"*.example.com"`) |
| `description` | string | Description                                 |
| `priority`    | int64  | Confidence / priority level                 |
| `disabled`    | bool   | Whether the definition is disabled          |

### Example — List all DNS classifications

```hcl
data "arubasdwan_app_dns_classifications" "all" {}

output "dns_apps" {
  value = data.arubasdwan_app_dns_classifications.all.dns_classifications
}
```

---

## Resource: `arubasdwan_app_compound_classification`

Manages a compound match-based application definition. Supports matching on any combination of IP, port, protocol, DNS, geo location, service, DSCP, and VLAN/interface.

> The Orchestrator does not enforce unique names, so creating (or renaming to) a name that a user-defined compound classification already uses is rejected (already at plan time) — it would add a duplicate definition, and policies reference applications by name. Import the existing definition instead.

### Arguments

| Argument      | Type   | Required | Default | Description                       |
|---------------|--------|----------|---------|-----------------------------------|
| `name`        | string | yes      |         | Application name                  |
| `description` | string | no       | `""`    | Description                       |
| `confidence`  | int64  | no       | `100`   | Confidence value (0-100)          |
| `disabled`    | bool   | no       | `false` | Whether disabled                  |

**Optional match criteria:**

| Argument        | Type   | Description                                              |
|-----------------|--------|----------------------------------------------------------|
| `protocol`      | string | Protocol (e.g. `"tcp"`, `"udp"`)                         |
| `src_ip`        | string | Source IP/subnet (e.g. `"10.0.0.0/8"`)                   |
| `dst_ip`        | string | Destination IP/subnet                                    |
| `either_ip`     | string | Either IP (mutually exclusive with src/dst)              |
| `src_port`      | string | Source port/range                                        |
| `dst_port`      | string | Destination port/range                                   |
| `either_port`   | string | Either port                                              |
| `src_dns`       | string | Source DNS pattern                                       |
| `dst_dns`       | string | Destination DNS pattern                                  |
| `either_dns`    | string | Either DNS pattern                                       |
| `src_geo`       | string | Source geo location                                      |
| `dst_geo`       | string | Destination geo location                                 |
| `either_geo`    | string | Either geo location                                      |
| `src_service`   | string | Source service                                           |
| `dst_service`   | string | Destination service                                      |
| `either_service`| string | Either service                                           |
| `dscp`          | string | DSCP value                                               |
| `vlan`          | string | Interface/VLAN                                           |

### Example

```hcl
resource "arubasdwan_app_compound_classification" "internal_api" {
  name        = "InternalAPI"
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
```

### Import

```bash
terraform import arubasdwan_app_compound_classification.internal_api 57
```

---

## Data Source: `arubasdwan_app_compound_classifications`

Retrieves all user-defined compound classification application definitions from the Orchestrator.

### Attributes

| Attribute                  | Type | Description                                          |
|----------------------------|------|------------------------------------------------------|
| `compound_classifications` | list | List of compound classification definition objects   |

Each object contains:

| Field           | Type   | Description                                            |
|-----------------|--------|--------------------------------------------------------|
| `id`            | string | Numeric ID assigned by the Orchestrator (as a string)  |
| `name`          | string | Application name                                       |
| `description`   | string | Description                                            |
| `confidence`    | int64  | Confidence value (0-100)                               |
| `disabled`      | bool   | Whether the definition is disabled                     |
| `protocol`      | string | Protocol match (e.g. `"tcp"`, `"udp"`)                 |
| `src_ip`        | string | Source IP/subnet match                                 |
| `dst_ip`        | string | Destination IP/subnet match                            |
| `either_ip`     | string | Either direction IP match                              |
| `src_port`      | string | Source port/range match                                |
| `dst_port`      | string | Destination port/range match                           |
| `either_port`   | string | Either direction port match                            |
| `src_dns`       | string | Source DNS pattern match                               |
| `dst_dns`       | string | Destination DNS pattern match                          |
| `either_dns`    | string | Either direction DNS match                             |
| `src_geo`       | string | Source geolocation match                               |
| `dst_geo`       | string | Destination geolocation match                          |
| `either_geo`    | string | Either direction geolocation match                     |
| `src_service`   | string | Source service match                                   |
| `dst_service`   | string | Destination service match                              |
| `either_service`| string | Either direction service match                         |
| `dscp`          | string | DSCP value match                                       |
| `vlan`          | string | Interface/VLAN match                                   |

### Example

```hcl
data "arubasdwan_app_compound_classifications" "all" {}

output "compound_apps" {
  value = data.arubasdwan_app_compound_classifications.all.compound_classifications
}
```

---

## Data Source: `arubasdwan_app_search`

Performs a server-side wildcard search across **all** application definitions on the Orchestrator (built-in + user-defined) and returns the matching application names. The names can be referenced directly in `application` or `app_group` fields of `arubasdwan_security_policy`.

This is the recommended way to discover and use **built-in applications** that are not exposed via the per-type data sources.

### Arguments

| Argument  | Type   | Required | Default | Description                                             |
|-----------|--------|----------|---------|---------------------------------------------------------|
| `pattern` | string | yes      |         | Substring pattern to search for (case-insensitive)      |
| `limit`   | int64  | no       | `0`     | Maximum number of results. `0` means no limit           |

### Attributes

| Attribute      | Type         | Description                          |
|----------------|--------------|--------------------------------------|
| `applications` | list(string) | List of matching application names   |

### Example

```hcl
data "arubasdwan_app_search" "skype" {
  pattern = "skype"
}

output "skype_apps" {
  value = data.arubasdwan_app_search.skype.applications
  # ["Skype", "Skypech", "SkypeForBusiness", ...]
}

# Use the first match directly in a policy
resource "arubasdwan_security_policy" "block_skype" {
  segment_pair   = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 25000
  action         = "deny"
  application    = data.arubasdwan_app_search.skype.applications[0]
}
```

### API Endpoint

| Method | Endpoint                                                  |
|--------|-----------------------------------------------------------|
| `POST` | `/gms/rest/applicationDefinition/applications/wildcard`   |

Request body: `{"pattern": "<substring>", "limit": <int>}`. Response: flat JSON array of strings.

---

## Resource: `arubasdwan_application_group`

Manages a user-defined application group on the Orchestrator. Application groups bundle multiple applications together and can be referenced by name in security policies via the `app_group` match field.

> Creating a group whose name already exists on the Orchestrator is rejected (already at plan time) — groups are keyed by name, so the API would silently replace the existing member list. Import the existing group instead.

### Arguments

| Argument | Type         | Required | Description                                                      |
|----------|--------------|----------|------------------------------------------------------------------|
| `name`   | string       | yes      | Group name. **Forces replacement on change** (name is the API key). |
| `apps`   | list(string) | yes      | List of application names to include in the group                |

### Attributes

| Attribute | Type   | Description                    |
|-----------|--------|--------------------------------|
| `id`      | string | Same as the group name         |

### Example

```hcl
resource "arubasdwan_application_group" "web_services" {
  name = "WebServices"
  apps = [
    arubasdwan_app_port_protocol.custom_https.name,
    "HTTP",
    "HTTPS",
  ]
}
```

### Import

Import by group name:

```bash
terraform import arubasdwan_application_group.web_services WebServices
```

---

## Data Source: `arubasdwan_application_groups`

Retrieves all user-defined application groups from the Orchestrator.

### Attributes

| Attribute            | Type | Description                           |
|----------------------|------|---------------------------------------|
| `application_groups` | list | List of application group objects     |

Each object contains:

| Field  | Type         | Description                    |
|--------|--------------|--------------------------------|
| `name` | string       | Group name                     |
| `apps` | list(string) | Application names in the group |

### Example

```hcl
data "arubasdwan_application_groups" "all" {}

output "app_groups" {
  value = data.arubasdwan_application_groups.all.application_groups
}
```

---

## Resource: `arubasdwan_ip_address_group`

Manages an IP address group on the Orchestrator. Address groups are reusable named collections of IP addresses/CIDRs and can be referenced by ACLs and security policies.

### Arguments

| Argument | Type | Required | Description                                                            |
|----------|------|----------|------------------------------------------------------------------------|
| `name`   | string | yes    | Unique name of the group. **Forces replacement on change.**            |
| `rules`  | list   | yes    | Ordered list of rules composing the group (see nested fields below)    |

Each entry in `rules` accepts:

| Field             | Type         | Required | Default | Description                                                |
|-------------------|--------------|----------|---------|------------------------------------------------------------|
| `included_ips`    | list(string) | no       | `[]`    | Included IPs/CIDRs (e.g. `["10.0.0.0/8", "192.168.1.5/32"]`) |
| `excluded_ips`    | list(string) | no       | `[]`    | Explicitly excluded IPs/CIDRs                              |
| `included_groups` | list(string) | no       | `[]`    | Names of nested address groups to include                  |
| `comment`         | string       | no       | `""`    | Free-form comment for this rule                            |

### Attributes

| Attribute | Type   | Description                |
|-----------|--------|----------------------------|
| `id`      | string | Same as the group name     |

### Example

```hcl
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
```

### Import

```bash
terraform import arubasdwan_ip_address_group.office_subnets OfficeSubnets
```

---

## Data Source: `arubasdwan_ip_address_groups`

Retrieves all IP address groups configured on the Orchestrator.

### Attributes

| Attribute        | Type | Description                       |
|------------------|------|-----------------------------------|
| `address_groups` | list | List of address group objects     |

Each object contains the same fields as the resource above (`name`, `rules` with `included_ips`/`excluded_ips`/`included_groups`/`comment`).

### Example

```hcl
data "arubasdwan_ip_address_groups" "all" {}

output "ip_groups" {
  value = data.arubasdwan_ip_address_groups.all.address_groups
}
```

---

## Data Source: `arubasdwan_vrf_segments`

Retrieves all VRF segments from the Orchestrator. Optionally resolves a `segment_pair` from VRF names, for use in security policy resources.

### Arguments

| Argument     | Type   | Required | Description                                      |
|--------------|--------|----------|--------------------------------------------------|
| `source_vrf` | string | no       | Source VRF name to resolve into a segment pair   |
| `dest_vrf`   | string | no       | Destination VRF name to resolve into a segment pair |

> Both `source_vrf` and `dest_vrf` must be provided together to resolve a `segment_pair`.

### Attributes

| Attribute       | Type   | Description                                                        |
|-----------------|--------|--------------------------------------------------------------------|
| `segment_pair`  | string | Resolved segment pair (e.g. `"0_1"`). Empty if VRF names not set. |
| `segments`      | list   | List of all VRF segment objects                                    |
| `zone_mappings` | list   | Zone-to-VRF assignments (zone IDs are unique per VRF)             |

Each object in `segments` contains:

| Field     | Type   | Description             |
|-----------|--------|-------------------------|
| `id`      | int64  | Segment ID              |
| `name`    | string | Segment name            |
| `status`  | int64  | Segment status          |
| `comment` | string | Segment comment         |

Each object in `zone_mappings` contains:

| Field       | Type   | Description                    |
|-------------|--------|--------------------------------|
| `zone_id`   | int64  | Zone ID (unique per VRF)       |
| `zone_name` | string | Zone name                      |
| `vrf_id`    | int64  | VRF segment ID                 |
| `vrf_name`  | string | VRF segment name               |

### Example — List all VRF segments

```hcl
data "arubasdwan_vrf_segments" "all" {}

output "vrfs" {
  value = data.arubasdwan_vrf_segments.all.segments
}
```

### Example — Resolve segment pair from VRF names

```hcl
data "arubasdwan_vrf_segments" "default_to_corporate" {
  source_vrf = "Default"
  dest_vrf   = "Corporate"
}

# Use the resolved segment_pair in a security policy
resource "arubasdwan_security_policy" "cross_vrf" {
  segment_pair   = data.arubasdwan_vrf_segments.default_to_corporate.segment_pair
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 30000
  action         = "allow"
}
```

---

## Data Source: `arubasdwan_appliance_deployments`

Retrieves all appliances with their deployment configuration: hostname, serial number, and every configured IP interface — management interfaces (`mgmt0`, `mgmt1`, …), WAN/LAN datapath interfaces (including VLAN sub-interfaces), and loopback interfaces. Each interface carries its deployment settings: interface label, VRF segment, security zone, firewall mode, and bandwidth limits. For WAN interfaces behind NAT (typically carrying private addresses), the public IP discovered by the Orchestrator is included. Each appliance additionally reports its SD-WAN region, the applied template groups, the Business Intent Overlays (BIO) it is associated with, its locally configured static routes, the configured EC license, and the system bandwidth. LAN interfaces with DHCP server or relay enabled include the full DHCP configuration.

The data is combined from `GET /gms/rest/appliance`, `GET /gms/rest/deployment`, `GET /gms/rest/virtualif/loopback`, `GET /gms/rest/tunnelsConfiguration/deployment`, `GET /gms/rest/regions{,/appliances}`, `GET /gms/rest/template/applianceAssociation`, `GET /gms/rest/gms/overlays/{association,config/regions}`, `GET /gms/rest/subnets/all`, `GET /gms/rest/license/portal/appliance`, and — for segment/zone name resolution — `GET /gms/rest/vrf/config/segments` and `GET /gms/rest/zones/vrfSegmentZonesMap`.

### Arguments

| Argument | Type   | Required | Description                                                                 |
|----------|--------|----------|-----------------------------------------------------------------------------|
| `ne_pk`  | string | no       | Appliance primary key (e.g. `"3.NE"`) to fetch a single appliance. If omitted, all appliances are returned. |
| `cached` | bool   | no       | Read deployment data from the Orchestrator database (`true`, default) or live from each appliance (`false`). Live reads are slower and fail for unreachable appliances. |
| `models` | list   | no       | Only include appliances with one of these models (case-insensitive exact match) |
| `exclude_models` | list | no  | Exclude appliances with one of these models (case-insensitive exact match) |
| `sites`  | list   | no       | Only include appliances tagged with one of these sites (case-insensitive exact match) |
| `exclude_sites` | list | no   | Exclude appliances tagged with one of these sites (case-insensitive exact match) |
| `hostname_regex` | string | no | Only include appliances whose hostname matches this RE2 regex |
| `exclude_hostname_regex` | string | no | Exclude appliances whose hostname matches this RE2 regex |

All filters are combined with AND and applied **before** any per-appliance data is fetched — excluded appliances are never queried.

```hcl
data "arubasdwan_appliance_deployments" "filtered" {
  exclude_models         = ["EC-V"]
  exclude_hostname_regex = "(?i)^(lab|test)-"
  sites                  = ["Berlin", "Hamburg"]
}
```

### Attributes

| Attribute    | Type | Description                                                  |
|--------------|------|--------------------------------------------------------------|
| `appliances` | list | List of appliance objects, sorted by hostname (see below)    |

Each object in `appliances` contains:

| Field              | Type   | Description                                                       |
|--------------------|--------|-------------------------------------------------------------------|
| `ne_pk`            | string | Orchestrator primary key of the appliance (e.g. `"3.NE"`)         |
| `hostname`         | string | Appliance hostname                                                |
| `serial`           | string | Hardware serial number                                            |
| `model`            | string | Appliance model (e.g. `"EC-S-B"`)                                 |
| `site`             | string | Site name the appliance is tagged with                            |
| `software_version` | string | ECOS software version                                             |
| `mode`             | string | Deployment mode (e.g. `"inline-router"`)                          |
| `network_role`     | string | Network role (e.g. `"0"` = spoke, `"1"` = hub)                    |
| `management_ip`    | string | IP address the Orchestrator uses to manage the appliance          |
| `region_id`        | int64  | SD-WAN region ID the appliance belongs to (`0` = Default region or regions not used) |
| `region_name`      | string | Resolved SD-WAN region name; empty if regions are not used        |
| `license`          | object | EC license (deployment config, filled in from the portal license assignment): `tier`, `tier_bandwidth`, `boost`, `boost_bandwidth` — bandwidth values passed through unchanged |
| `system_bandwidth_outbound` | int64 | System maximum outbound bandwidth in Kbps (`0` = not set)   |
| `system_bandwidth_inbound`  | int64 | System maximum inbound bandwidth in Kbps (`0` = not set)    |
| `template_groups`  | list   | Names of the template groups applied to the appliance (sorted)    |
| `overlays`         | list   | Business Intent Overlays (BIO) the appliance is associated with (see below) |
| `static_routes`    | list   | Locally configured static routes of the appliance (see below)     |
| `interfaces`       | list   | All configured IP interfaces (see below)                          |

Each object in `overlays` contains:

| Field  | Type   | Description                                                    |
|--------|--------|----------------------------------------------------------------|
| `id`   | string | Numeric overlay ID as reported by the Orchestrator (e.g. `"1"`) |
| `name` | string | Overlay name (e.g. `"RealTime"`); empty if it cannot be resolved |

Each object in `static_routes` contains (learned and system-generated routes are excluded):

| Field       | Type   | Description                                                 |
|-------------|--------|-------------------------------------------------------------|
| `prefix`    | string | Destination prefix in CIDR notation (e.g. `"10.20.0.0/16"`) |
| `next_hop`  | string | Next hop IP address; empty if not applicable                |
| `interface` | string | Egress interface name; empty if not applicable              |
| `metric`    | int64  | Route metric (lower value = higher priority)                |
| `vrf_id`    | int64  | VRF segment ID the route belongs to (`0` = Default)         |
| `vrf_name`  | string | Resolved VRF segment name                                   |
| `advertise` | bool   | Route is advertised to SD-WAN peers                         |

Each object in `interfaces` contains:

| Field           | Type   | Description                                                                                   |
|-----------------|--------|-----------------------------------------------------------------------------------------------|
| `name`          | string | Interface name (e.g. `"mgmt0"`, `"wan0"`, `"wan0.100"`, `"lan0"`, `"loopback100"`)             |
| `type`          | string | `"mgmt"`, `"wan"`, `"lan"`, `"loopback"`, or `"other"`                                        |
| `ip_address`    | string | Configured IP address (without prefix length)                                                 |
| `prefix_length` | int64  | Network mask as prefix length (e.g. `24`)                                                     |
| `cidr`          | string | IP in CIDR notation (e.g. `"10.1.2.3/24"`)                                                    |
| `label`         | string | Interface label name (e.g. `"INET1"`, `"MPLS"`); empty if none assigned                       |
| `vlan`          | string | VLAN ID for VLAN sub-interfaces; empty otherwise                                              |
| `dhcp`          | bool   | Interface obtains its address dynamically; for DHCP WAN interfaces the current address seen by the Orchestrator is reported |
| `next_hop`      | string | Configured next hop / gateway IP (WAN gateway; mgmt gateway for mgmt interfaces; current gateway for DHCP WANs); empty if none |
| `next_hop_is_private` | bool | `next_hop` is private / not globally routable (same ranges as `is_private`); `false` when `next_hop` is empty |
| `behind_nat`    | bool   | The Orchestrator considers this WAN interface to be behind a NAT device                       |
| `public_ip`     | string | Public IP discovered by the Orchestrator for this WAN interface (the "discovered IP"); empty if none |
| `is_private`    | bool   | `ip_address` is private / not globally routable — IPv4: RFC1918, CGNAT (RFC6598), link-local, loopback; IPv6: ULA (RFC4193), link-local, loopback |
| `vrf_id`        | int64  | VRF segment ID the interface is assigned to (`0` = Default); always `0` for mgmt interfaces, reported for loopbacks from Orchestrator 9.7.0 |
| `vrf_name`      | string | Resolved VRF segment name (e.g. `"Default"`); empty for mgmt interfaces and for loopbacks before Orchestrator 9.7.0 |
| `zone_id`       | int64  | Security zone ID assigned to the interface (`0` = no zone)                                    |
| `zone_name`     | string | Resolved security zone name; empty if no zone assigned                                        |
| `firewall_mode` | string | Firewall mode of WAN interfaces: `"allow-all"`, `"hardened"`, `"stateful"`, `"stateful-snat"`; empty for non-WAN interfaces |
| `max_bandwidth_outbound` | int64 | Maximum outbound (LAN → WAN) bandwidth in Kbps from the per-interface shaper, falling back to the Orchestrator's resolved view; `0` if not set (WAN only) |
| `max_bandwidth_inbound`  | int64 | Maximum inbound (WAN → LAN) bandwidth in Kbps from the per-interface shaper, falling back to the Orchestrator's resolved view; `0` if not set (WAN only)  |
| `dhcp_config`   | object | DHCP server/relay configuration of the LAN interface; `null` when neither is enabled (see below) |

`dhcp_config` contains `mode` (`"server"` or `"relay"`). In server mode: `prefix`, `ip_start`, `ip_end`, `ranges` (list of `{start, end}`), `gateways`, `dns_servers`, `ntp_servers`, `netbios_servers`, `netbios_node_type`, `default_lease`, `max_lease`, `options` (map keyed by DHCP option ID), `failover`, and `reservations` (list of `{hostname, ip, mac}`). In relay mode: `dhcp_servers`, `option82`, and `option82_policy`.

The `interfaces` list contains **one entry per configured IP address**, ordered: management interfaces first, then WAN/LAN in deployment order, then loopbacks. Interfaces without a configured IP address are omitted. The interface `name` is **not unique** within the list — a dual-stack interface appears once per address family (IPv4 and IPv6). When building maps keyed by `name`, group values with the HCL ellipsis operator (`{ for i in a.interfaces : i.name => i.cidr... }`) or include the `cidr` in the key.

> **Note:** If auxiliary data (loopback interfaces, discovered public IPs, or label names) cannot be fetched for individual appliances, the read succeeds with Terraform warnings and the affected fields stay empty. Failures reading the appliance inventory or deployment itself are hard errors, so a partial inventory is never silently reported as complete.

### Example — Inventory overview

```hcl
data "arubasdwan_appliance_deployments" "all" {}

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
```

### Example — Discovered public IPs of NAT'ed WAN interfaces

```hcl
data "arubasdwan_appliance_deployments" "all" {}

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
```

### Example — WAN interface deployment details

```hcl
data "arubasdwan_appliance_deployments" "all" {}

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
```

### Example — Region, applied templates/overlays, and static routes

```hcl
data "arubasdwan_appliance_deployments" "all" {}

output "applied_config" {
  value = {
    for a in data.arubasdwan_appliance_deployments.all.appliances :
    a.hostname => {
      region          = a.region_name
      template_groups = a.template_groups
      overlays        = [for o in a.overlays : o.name]
      static_routes = [
        for r in a.static_routes :
        "${r.prefix} via ${r.next_hop} (${r.vrf_name}, metric ${r.metric})"
      ]
    }
  }
}
```

---

## Data Source: `arubasdwan_vrrp_instances`

Retrieves the VRRP instances configured on the appliances: per appliance the group ID (VRID), peering interface, virtual IP address, priority, preemption, and timers, plus the operational state (Master/Backup/Init, current master IP, transitions, uptime, virtual MAC). Appliances without VRRP configuration are included with an empty `vrrp_instances` list.

The data is combined from `GET /gms/rest/appliance` (inventory) and `GET /gms/rest/vrrp` (VRRP instances per appliance).

### Arguments

The data source supports the same appliance filters as `arubasdwan_appliance_deployments`:

| Argument | Type   | Required | Description                                                                 |
|----------|--------|----------|-----------------------------------------------------------------------------|
| `ne_pk`  | string | no       | Appliance primary key (e.g. `"3.NE"`) to fetch a single appliance. If omitted, all appliances are returned. |
| `cached` | bool   | no       | Read VRRP data from the Orchestrator database (`true`, default) or live from each appliance (`false`). Live reads are slower and fail for unreachable appliances. |
| `models` | list   | no       | Only include appliances with one of these models (case-insensitive exact match) |
| `exclude_models` | list | no  | Exclude appliances with one of these models (case-insensitive exact match) |
| `sites`  | list   | no       | Only include appliances tagged with one of these sites (case-insensitive exact match) |
| `exclude_sites` | list | no   | Exclude appliances tagged with one of these sites (case-insensitive exact match) |
| `hostname_regex` | string | no | Only include appliances whose hostname matches this RE2 regex |
| `exclude_hostname_regex` | string | no | Exclude appliances whose hostname matches this RE2 regex |

All filters are combined with AND and applied **before** any per-appliance data is fetched — excluded appliances are never queried.

### Attributes

| Attribute    | Type | Description                                                  |
|--------------|------|--------------------------------------------------------------|
| `appliances` | list | List of appliance objects, sorted by hostname (see below)    |

Each object in `appliances` contains:

| Field            | Type   | Description                                               |
|------------------|--------|-----------------------------------------------------------|
| `ne_pk`          | string | Orchestrator primary key of the appliance (e.g. `"3.NE"`) |
| `hostname`       | string | Appliance hostname                                        |
| `serial`         | string | Hardware serial number                                    |
| `model`          | string | Appliance model (e.g. `"EC-S-B"`)                         |
| `site`           | string | Site name the appliance is tagged with                    |
| `vrrp_instances` | list   | VRRP instances of the appliance, sorted by interface and group ID; empty if none are configured (see below) |

Each object in `vrrp_instances` contains:

| Field                 | Type   | Description                                                        |
|-----------------------|--------|--------------------------------------------------------------------|
| `group_id`            | int64  | VRRP group ID (VRID) shared by the two peers of the group (1-255)  |
| `interface`           | string | Interface the instance is peering on (e.g. `"lan0"`)               |
| `virtual_ip`          | string | Virtual IP address of the VRRP group                               |
| `priority`            | int64  | VRRP priority (1-254); the peer with the higher priority is the master |
| `enabled`             | bool   | Instance is administratively up                                    |
| `preempt`             | bool   | Higher-priority peer takes the master role back when it returns    |
| `holddown`            | int64  | Holddown timer in seconds                                          |
| `advertisement_timer` | int64  | Time interval between VRRP advertisements in seconds               |
| `description`         | string | Description string of the instance                                 |
| `auth`                | string | VRRP authentication string (sensitive); may be empty or masked by the Orchestrator |
| `state`               | string | Operational state: `"Master"`, `"Backup"`, or `"Init"` (initializing, disabled, or interface down) |
| `master_ip`           | string | Interface or local IP address of the current VRRP master           |
| `master_transitions`  | int64  | Number of Master/Backup transitions; a high number indicates a problematic VRRP setup |
| `uptime`              | string | Time elapsed in the current state (e.g. `"0 days 11 hrs 49 mins 41 secs"`) |
| `virtual_mac`         | string | MAC address the instance is using (`00-00-5E-00-01-{VRID}` on hardware appliances, the interface MAC on virtual appliances) |
| `vip_owner`           | bool   | Appliance owns the virtual IP; always `false` on EdgeConnect appliances |
| `packet_trace`        | bool   | VRRP packet tracing is enabled                                     |

### Example — VRRP overview per appliance

```hcl
data "arubasdwan_vrrp_instances" "all" {}

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
```

### Example — Virtual IPs for documentation exports (e.g. NetBox FHRP groups)

```hcl
data "arubasdwan_vrrp_instances" "all" {}

output "virtual_ips" {
  value = distinct(flatten([
    for a in data.arubasdwan_vrrp_instances.all.appliances : [
      for v in a.vrrp_instances : v.virtual_ip
    ]
  ]))
}
```

---

## Data Source: `arubasdwan_bgp_config`

Retrieves the BGP configuration of the appliances: per appliance and VRF segment the system settings (local ASN, router ID, graceful restart, OSPF redistribution) and the configured neighbors (peer IP, remote ASN, peer type, timers, route policies, BFD). Appliances without BGP configuration are included with an empty `vrfs` list.

The data is combined from `GET /gms/rest/appliance` (inventory) and `GET /gms/rest/bgp/config/allVrfs/system` plus `GET /gms/rest/bgp/config/allVrfs/neighbor` (BGP configuration per appliance and VRF). Orchestrator versions without the allVrfs endpoints fall back to the default-VRF endpoints `/gms/rest/bgp/config/system` and `/gms/rest/bgp/config/neighbor`.

### Arguments

The data source supports the same appliance filters as `arubasdwan_appliance_deployments`:

| Argument | Type   | Required | Description                                                                 |
|----------|--------|----------|-----------------------------------------------------------------------------|
| `ne_pk`  | string | no       | Appliance primary key (e.g. `"3.NE"`) to fetch a single appliance. If omitted, all appliances are returned. |
| `cached` | bool   | no       | Read BGP data from the Orchestrator database (`true`, default) or live from each appliance (`false`). Live reads are slower and fail for unreachable appliances. |
| `models` | list   | no       | Only include appliances with one of these models (case-insensitive exact match) |
| `exclude_models` | list | no  | Exclude appliances with one of these models (case-insensitive exact match) |
| `sites`  | list   | no       | Only include appliances tagged with one of these sites (case-insensitive exact match) |
| `exclude_sites` | list | no   | Exclude appliances tagged with one of these sites (case-insensitive exact match) |
| `hostname_regex` | string | no | Only include appliances whose hostname matches this RE2 regex |
| `exclude_hostname_regex` | string | no | Exclude appliances whose hostname matches this RE2 regex |

All filters are combined with AND and applied **before** any per-appliance data is fetched — excluded appliances are never queried.

### Attributes

| Attribute    | Type | Description                                                  |
|--------------|------|--------------------------------------------------------------|
| `appliances` | list | List of appliance objects, sorted by hostname (see below)    |

Each object in `appliances` contains:

| Field      | Type   | Description                                               |
|------------|--------|-----------------------------------------------------------|
| `ne_pk`    | string | Orchestrator primary key of the appliance (e.g. `"3.NE"`) |
| `hostname` | string | Appliance hostname                                        |
| `serial`   | string | Hardware serial number                                    |
| `model`    | string | Appliance model (e.g. `"EC-S-B"`)                         |
| `site`     | string | Site name the appliance is tagged with                    |
| `vrfs`     | list   | BGP configuration per VRF segment, sorted by VRF ID; empty if the appliance has no BGP configuration (see below) |

Each object in `vrfs` contains:

| Field       | Type   | Description                                                        |
|-------------|--------|--------------------------------------------------------------------|
| `vrf_id`    | int64  | VRF segment ID the configuration belongs to (0 = Default)          |
| `vrf_name`  | string | Resolved VRF segment name (e.g. `"Default"`); empty if it cannot be resolved |
| `system`    | object | BGP system (process) configuration of the VRF segment (see below)  |
| `neighbors` | list   | BGP neighbors of the VRF segment, sorted by peer IP; empty if none are configured (see below) |

The `system` object contains:

| Field                      | Type   | Description                                                       |
|----------------------------|--------|-------------------------------------------------------------------|
| `enabled`                  | bool   | BGP is enabled in this VRF segment                                |
| `asn`                      | int64  | Local autonomous system number (4-byte ASNs supported)            |
| `router_id`                | string | BGP router ID                                                     |
| `graceful_restart`         | bool   | Graceful restart is enabled                                       |
| `max_restart_time`         | int64  | Max wait in seconds for a restarting peer before its routes are removed (1-3600) |
| `stale_path_time`          | int64  | Max time in seconds stale routes of a restarted peer are kept (1-3600) |
| `redistribute_ospf`        | bool   | BGP routes are redistributed to OSPF                              |
| `redistribute_ospf_filter` | int64  | Filter bitmask applied to routes redistributed to OSPF            |
| `remote_as_path_advertise` | bool   | The remote AS path is propagated when advertising routes          |

Each object in `neighbors` contains:

| Field                | Type   | Description                                                        |
|----------------------|--------|--------------------------------------------------------------------|
| `ip`                 | string | IP address of the neighbor                                         |
| `remote_as`          | int64  | Remote autonomous system number (4-byte ASNs supported)            |
| `type`               | string | Peer type (e.g. `"Branch"`, `"Branch-transit"`, `"PE-router"`)     |
| `enabled`            | bool   | BGP session to this neighbor is enabled                            |
| `import_routes`      | bool   | Routes learned from the neighbor are imported                      |
| `export_map`         | int64  | Route export policies bitmask; `4294967295` = predefined bitmask of the peer type unchanged |
| `hold_timer`         | int64  | Hold timer in seconds                                              |
| `keepalive_timer`    | int64  | Interval in seconds between KEEPALIVE messages                     |
| `med`                | int64  | Multi-Exit Discriminator for routes advertised to the neighbor     |
| `inbound_med`        | int64  | Metric applied to routes received from the neighbor                |
| `local_preference`   | int64  | Local preference for routes advertised to the neighbor             |
| `as_prepend_count`   | int64  | Number of additional times the local AS is prepended to the AS path |
| `next_hop_self`      | bool   | The appliance advertises its own IP address as next hop            |
| `directly_connected` | bool   | Peer adjacency is treated as single hop (`false` = multi hop)      |
| `bfd_enabled`        | bool   | A BFD session is desired for this peer                             |
| `evpn`               | bool   | EVPN is enabled for this peer                                      |
| `password`           | string | MD5 password of the session (sensitive); may be empty or masked by the Orchestrator |

### Example — BGP overview per appliance

```hcl
data "arubasdwan_bgp_config" "all" {}

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
```

### Example — All remote AS numbers peered with

```hcl
data "arubasdwan_bgp_config" "all" {}

output "remote_as_numbers" {
  value = distinct(flatten([
    for a in data.arubasdwan_bgp_config.all.appliances : [
      for v in a.vrfs : [
        for n in v.neighbors : n.remote_as
      ]
    ]
  ]))
}
```

---

## Data Source: `arubasdwan_overlays`

Retrieves all Business Intent Overlays (BIO) via `GET /gms/rest/gms/overlays/config`, including how each overlay selects traffic and, for the ACL built into an overlay, its individual entries.

### Attributes

| Attribute  | Type | Description                            |
|------------|------|----------------------------------------|
| `overlays` | list | List of overlay objects, sorted by ID  |

Each object in `overlays` contains:

| Field             | Type   | Description                                                                 |
|-------------------|--------|-----------------------------------------------------------------------------|
| `id`              | int64  | Numeric overlay ID                                                          |
| `name`            | string | Overlay name (e.g. `"Business"`)                                            |
| `match_type`      | string | `"overlay_acl"`, `"appliance_acl"`, `"interface_label"`, or empty           |
| `interface_label` | string | LAN interface label (only for `interface_label`)                            |
| `acl_name`        | string | ACL name (for `overlay_acl` / `appliance_acl`)                              |
| `acl_entries`     | list   | Entries of the built-in ACL: `sequence`, `permit`, `match_all`, `application`, `app_group`, `comment` |
| `acl_raw`         | string | The built-in ACL exactly as stored by the Orchestrator                      |

### Example

```hcl
data "arubasdwan_overlays" "all" {}

output "overlay_matching" {
  value = {
    for o in data.arubasdwan_overlays.all.overlays : o.name => {
      id         = o.id
      match_type = o.match_type
      entries = [
        for e in o.acl_entries :
        "${e.sequence}: ${e.permit ? "permit" : "deny"} ${e.match_all ? "all" : coalesce(e.application, e.app_group, "")}"
      ]
    }
  }
}
```

---

## Resource: `arubasdwan_overlay_acl`

Manages the ACL built into a Business Intent Overlay — the rules that decide which traffic the overlay carries. Each entry matches an **application**, an **application group**, or **all traffic**, so applications and groups managed by this provider can be attached to an overlay.

The overlay itself must already exist. Only its match configuration is replaced (`PUT /gms/rest/gms/overlays/config?overlayId=<id>`): the overlay is re-read immediately before each write and every other setting — bonding policy, topology, WAN ports, internet policy, and anything this provider does not model — is written back unchanged.

Works on Orchestrator 9.6.3 and 9.7.0; the overlay API is identical in both.

### Arguments

| Argument       | Type   | Required | Description                                                              |
|----------------|--------|----------|--------------------------------------------------------------------------|
| `overlay_name` | string | yes      | Name of the existing overlay whose ACL is managed (forces replacement)   |
| `entries`      | set    | yes      | The ACL entries; order in the configuration is irrelevant (see below)    |
| `acl_name`     | string | no       | ACL name inside the overlay; defaults to the existing one or `Overlay_<overlay_name>` |
| `allow_entry_removal` | bool | no | Confirm removal of entries Terraform never created (default `false` = abort). Not needed for removing entries this resource created itself |

Each object in `entries` (a set — the `sequence` attribute determines the evaluation order, not the position in the configuration):

| Field         | Type   | Required | Description                                                                  |
|---------------|--------|----------|------------------------------------------------------------------------------|
| `sequence`    | int64  | yes      | Evaluation order within the ACL; must be unique                              |
| `permit`      | bool   | no       | Carry matching traffic in this overlay (default `true`; `false` = deny)      |
| `match_all`   | bool   | no       | Explicit catch-all; cannot be combined with other criteria (default `false`) |
| `comment`     | string | no       | Free-form comment                                                            |

Match criteria (all optional, combined with AND within one entry): `application`, `app_group`, `src_ip`, `dst_ip`, `either_ip`, `src_port`, `dst_port`, `either_port`, `protocol`, `dscp`, `src_dns`, `dst_dns`, `either_dns`, `src_service`, `dst_service`, `either_service`, `src_address_group`, `dst_address_group`, `either_address_group`, `src_vrf`, `dst_vrf`. Multi-value fields take comma-separated lists.

> Each entry needs at least one match criterion, or `match_all = true`.

> **The resource owns the overlay's complete ACL and refuses to delete entries it never created.** Creating it for an overlay that already has an ACL — or for an overlay that does not exist — fails at **plan** time, so nothing is written before the problem surfaces. Removing an entry a previous apply created is a deliberate change and proceeds with a warning. An entry that appeared without Terraform — a rule added in the UI, say — aborts plan and apply; adopt it by adding it to `entries`, or set `allow_entry_removal = true` to confirm its removal. Removals are always reported as a warning, because Terraform's plan summary counts resources rather than ACL entries. For an overlay that already has entries, import it first (`terraform import arubasdwan_overlay_acl.<name> <overlay_name>`) and review the plan; creating the resource for a populated overlay is rejected with an error. The ACL's `options` object and entry fields this provider does not model are preserved across updates.

### Attributes

| Attribute    | Type   | Description                          |
|--------------|--------|--------------------------------------|
| `id`         | string | Resource identifier (overlay name)   |
| `overlay_id` | int64  | Numeric overlay ID                   |

### Example

```hcl
resource "arubasdwan_application_group" "critical" {
  name = "CriticalApps"
  apps = ["salesforce", "office365"]
}

resource "arubasdwan_app_dns_classification" "internal_crm" {
  name   = "internal-crm"
  domain = "crm.example.com"
}

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
    },
  ]
}
```

### Import

```bash
terraform import arubasdwan_overlay_acl.business Business
```

> **Destroying the resource resets the overlay to matching all traffic** (a single `match_all` entry) — the state of a freshly created overlay. The overlay itself is never deleted.

---

## Resource: `arubasdwan_app_address_map`

Manages an address map: an IPv4 address range classified as a named application, so security policies and overlay ACLs can match traffic to that range by name. The Orchestrator API calls these entries IP intelligence classifications; the UI lists them under *Address Map*.

Addresses are written in dotted notation — the provider converts them to the 32-bit integers the API expects. For a single host, set `ip_end` to the same value as `ip_start`.

### Arguments

| Argument       | Type   | Required | Description                                                                 |
|----------------|--------|----------|-----------------------------------------------------------------------------|
| `name`         | string | yes      | Application name; letters, digits, hyphen, underscore, period, max 31 chars |
| `ip_start`     | string | yes      | First IPv4 address of the range (forces replacement)                        |
| `ip_end`       | string | yes      | Last IPv4 address of the range; same as `ip_start` for a single host (forces replacement) |
| `description`  | string | no       | Description (no pipe characters or line breaks)                             |
| `country`      | string | no       | Country name associated with the range                                      |
| `country_code` | string | no       | Two-letter ISO 3166-1 alpha-2 country code                                  |
| `org`          | string | no       | Organization associated with the range                                      |
| `priority`     | int64  | no       | Classification priority; higher wins (default `100`)                        |

### Attributes

| Attribute    | Type   | Description                                                        |
|--------------|--------|--------------------------------------------------------------------|
| `id`         | string | The address for a single-address range, otherwise `<ip_start>-<ip_end>` |
| `service_id` | int64  | Service ID assigned by the Orchestrator                            |

> Overlay ACLs and policies match an address map through their **service** criteria (`either_service`, or `src_service`/`dst_service`) — that is what the Orchestrator UI fills when an address map is selected. Those criteria also match the `org` value, so one rule can select every range of an organization.

> **Ranges must not overlap.** A range overlapping an existing entry is rejected at plan time, since the Orchestrator classifies an address by the entry covering it and overlaps make that ambiguous. Adjacent ranges are fine. Two overlapping entries in the *same* configuration surface during apply — a provider cannot inspect sibling resources while planning.

> **The address range is the identifier.** The Orchestrator treats the create call as an upsert, so applying a range that already exists would silently overwrite it — creating the resource for an existing range is rejected at plan time. Import it instead. A name already used by another address map is rejected as well, since policies and overlay ACLs reference applications by name.

### Example

```hcl
resource "arubasdwan_app_address_map" "stream_server" {
  name         = "stream-server"
  ip_start     = "10.0.13.72"
  ip_end       = "10.0.13.72"
  description  = "Streaming server, on-premises"
  country_code = "DE"
  org          = "Example Org"
}

resource "arubasdwan_overlay_acl" "business" {
  overlay_name = "Business"

  entries = [
    {
      sequence    = 1000
      either_service = arubasdwan_app_address_map.stream_server.name
    },
  ]
}
```

### Import

```bash
terraform import arubasdwan_app_address_map.stream_server 10.0.13.72
terraform import arubasdwan_app_address_map.partner_range 203.0.113.10-203.0.113.19
```

---

## Data Source: `arubasdwan_app_address_maps`

Retrieves all user-defined address maps.

### Attributes

| Attribute      | Type | Description                                                |
|----------------|------|------------------------------------------------------------|
| `address_maps` | list | Address map objects, sorted by the start address of their range |

Each object contains `name`, `ip_start`, `ip_end`, `description`, `country`, `country_code`, `org`, `priority`, and `service_id`.

### Example

```hcl
data "arubasdwan_app_address_maps" "all" {}

output "address_maps" {
  value = {
    for m in data.arubasdwan_app_address_maps.all.address_maps :
    m.name => m.ip_start == m.ip_end ? m.ip_start : "${m.ip_start}-${m.ip_end}"
  }
}
```

---

## Resource: `arubasdwan_security_policy`

Manages a single security policy rule on the Orchestrator. Policies are scoped to a segment pair and identified by the combination of source zone, destination zone, and priority.

### Arguments

**Required:**

| Argument         | Type   | Description                                      |
|------------------|--------|--------------------------------------------------|
| `segment_pair`   | string | Segment pair identifier (e.g. `"0_0"`)           |
| `source_zone_id` | int64  | Source security zone ID                           |
| `dest_zone_id`   | int64  | Destination security zone ID                     |
| `priority`       | int64  | Rule priority (20000–65535; lower = higher priority) |
| `action`         | string | Action to take: `"allow"` or `"deny"`            |

**Optional — Rule settings:**

| Argument       | Type   | Allowed values            | Default for new rules | Description                        |
|----------------|--------|---------------------------|-----------------------|------------------------------------|
| `rule_state`   | string | `"enable"`, `"disable"`   | `"enable"`            | Enable or disable the rule         |
| `logging`      | string | `"enable"`, `"disable"`   | `"disable"`           | Enable or disable logging          |
| `log_priority` | string | `"0"`–`"7"` (syslog)      | `"0"`                 | Syslog priority level              |
| `comment`      | string | any                       | `""`                  | Comment for the rule               |

> **Important — `logging` / `rule_state` / `log_priority` use `UseStateForUnknown` semantics, not Terraform Defaults.** When you omit one of these in HCL, the existing Orchestrator value is **preserved** rather than reverted to a Terraform default. The "default" column above only applies when the resource is *first created* via Terraform without specifying the value.
>
> **Cross-field rule:** when `logging = "enable"`, `log_priority` must also be set explicitly in the configuration. The provider rejects the plan otherwise.
>
> **Validation:** `action` ∈ {`"allow"`, `"deny"`} — `priority` must be 20000–65535 — values outside the allowed sets are rejected at plan time.

**Optional — Match criteria (network):**

| Argument      | Type   | Description                                                        |
|---------------|--------|--------------------------------------------------------------------|
| `acl`         | string | ACL class name to match                                            |
| `src_ip`      | string | Source IP/subnet (e.g. `"192.168.1.0/24"`, `"10.0.0.1-100"`)      |
| `dst_ip`      | string | Destination IP/subnet                                              |
| `either_ip`   | string | Either source or destination IP (mutually exclusive with src/dst)  |
| `src_port`    | string | Source port or range (e.g. `"80"`, `"1024-65535"`)                 |
| `dst_port`    | string | Destination port or range                                          |
| `either_port` | string | Either source or destination port                                  |
| `protocol`    | string | Protocol to match (`"tcp"`, `"udp"`, `"ip"`, or number)           |

> **Multi-value IP/port fields** — the Orchestrator API uses `|` as the separator for lists. The provider lets you write the more familiar **comma-separated** form in HCL (e.g. `dst_port = "443,8443"` or `dst_ip = "10.0.0.0/8,192.168.1.0/24"`) and translates to/from `|` transparently. State and plan diffs are stable in the comma form.

**Optional — Match criteria (application):**

| Argument      | Type   | Description                                                                 |
|---------------|--------|-----------------------------------------------------------------------------|
| `application` | string | Application name to match (reference via `arubasdwan_app_port_protocol.name`) |
| `app_group`   | string | Application group name to match (reference via `arubasdwan_application_group.name`) |

**Optional — Match criteria (domain/DNS):**

| Argument    | Type   | Description                                           |
|-------------|--------|-------------------------------------------------------|
| `src_dns`   | string | Source DNS/domain pattern                              |
| `dst_dns`   | string | Destination DNS/domain pattern                         |
| `either_dns`| string | Either DNS pattern (supports wildcards: `"*google.com"`) |

**Optional — Match criteria (geo location):**

| Argument    | Type   | Description                                    |
|-------------|--------|------------------------------------------------|
| `src_geo`   | string | Source geo location (e.g. `"US"`, `"DE"`)      |
| `dst_geo`   | string | Destination geo location                        |
| `either_geo`| string | Either geo location                             |

**Optional — Match criteria (IP address groups):**

Reference an `arubasdwan_ip_address_group` by name. The Orchestrator wires these to `src_addrgrp_groups` / `dst_addrgrp_groups` / `either_addrgrp_groups` in the underlying API payload.

| Argument               | Type   | Description                                              |
|------------------------|--------|----------------------------------------------------------|
| `src_address_group`    | string | Source IP address group reference                         |
| `dst_address_group`    | string | Destination IP address group reference                    |
| `either_address_group` | string | Match either direction against an IP address group        |

**Optional — Match criteria (service/other):**

| Argument        | Type   | Description                                   |
|-----------------|--------|-----------------------------------------------|
| `src_service`   | string | Source service (SaaS app name/organization)   |
| `dst_service`   | string | Destination service                            |
| `either_service`| string | Either service                                 |
| `dscp`          | string | DSCP value to match                            |
| `vlan`          | string | Interface/VLAN to match (e.g. `"lan0"`)        |
| `overlay`       | string | Overlay to match                               |

### Attributes

| Attribute | Type   | Description                                                        |
|-----------|--------|--------------------------------------------------------------------|
| `id`      | string | Composite ID: `segment_pair/srcZone_dstZone/priority`              |

### Example

```hcl
# Allow traffic from an IP address group
resource "arubasdwan_security_policy" "allow_office" {
  segment_pair      = "0_0"
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

# Allow traffic matching a custom application group
resource "arubasdwan_security_policy" "allow_web_services" {
  segment_pair   = "0_0"
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 30000
  action         = "allow"
  app_group      = arubasdwan_application_group.web_services.name
  comment        = "Allow web services group"
}

# Allow a specific custom application
resource "arubasdwan_security_policy" "allow_custom_https" {
  segment_pair   = "0_0"
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 30100
  action         = "allow"
  application    = arubasdwan_app_port_protocol.custom_https.name
}

# Block traffic from specific geo location
resource "arubasdwan_security_policy" "geo_block" {
  segment_pair   = "0_0"
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 20000
  action         = "deny"
  dst_geo        = "CN"
}

# Match by DNS pattern
resource "arubasdwan_security_policy" "allow_google" {
  segment_pair   = "0_0"
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 25000
  action         = "allow"
  either_dns     = "*google.com"
}

# Default deny rule at lowest priority
resource "arubasdwan_security_policy" "default_deny" {
  segment_pair   = "0_0"
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 65535
  action         = "deny"
}
```

### Import

Import uses the composite ID format `segment_pair/srcZone_dstZone/priority`:

```bash
terraform import arubasdwan_security_policy.allow_web 0_0/20_21/1000
```

---

## Data Source: `arubasdwan_security_policies`

Retrieves all security policies for a given segment pair.

### Arguments

| Argument       | Type   | Required | Description                              |
|----------------|--------|----------|------------------------------------------|
| `segment_pair` | string | yes      | Segment pair to query (e.g. `"0_0"`)    |

### Attributes

| Attribute           | Type | Description                        |
|---------------------|------|------------------------------------|
| `security_policies` | list | List of security policy objects    |

Each object contains all the fields documented in the resource above (`source_zone_id`, `dest_zone_id`, `priority`, `action`, match criteria, etc.).

### Example

```hcl
data "arubasdwan_security_policies" "default" {
  segment_pair = "0_0"
}

output "all_policies" {
  value = data.arubasdwan_security_policies.default.security_policies
}
```

---

## Complete Example

This example shows how all resources work together:

```hcl
# Resolve VRF segment pair by name
data "arubasdwan_vrf_segments" "default" {
  source_vrf = "Default"
  dest_vrf   = "Default"
}

# Security Zones
resource "arubasdwan_security_zone" "production" {
  name = "Production"
}

resource "arubasdwan_security_zone" "development" {
  name = "Development"
}

# Application Definitions
resource "arubasdwan_app_port_protocol" "internal_api" {
  name        = "InternalAPI"
  port        = 8443
  protocol    = 6
  description = "Internal API service"
}

resource "arubasdwan_app_port_protocol" "monitoring" {
  name        = "Monitoring"
  port        = 9090
  protocol    = 6
  description = "Prometheus monitoring"
}

# Application Group
resource "arubasdwan_application_group" "infra_services" {
  name = "InfraServices"
  apps = [
    arubasdwan_app_port_protocol.internal_api.name,
    arubasdwan_app_port_protocol.monitoring.name,
  ]
}

# Security Policies — using resolved segment_pair
resource "arubasdwan_security_policy" "allow_infra" {
  segment_pair   = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 30000
  action         = "allow"
  app_group      = arubasdwan_application_group.infra_services.name
  comment        = "Allow infrastructure services"
}

resource "arubasdwan_security_policy" "default_deny" {
  segment_pair   = data.arubasdwan_vrf_segments.default.segment_pair
  source_zone_id = arubasdwan_security_zone.production.id
  dest_zone_id   = arubasdwan_security_zone.development.id
  priority       = 65535
  action         = "deny"
  comment        = "Default deny"
}
```

---

## API Endpoints Used

This provider communicates with the following Orchestrator REST API endpoints:

### Security Zones

| Method   | Endpoint                             | Description                    |
|----------|--------------------------------------|--------------------------------|
| `GET`    | `/gms/rest/zones`                    | List all security zones        |
| `POST`   | `/gms/rest/zones`                    | Add/edit security zones        |
| `GET`    | `/gms/rest/zones/nextId`             | Get next available zone ID     |

### Security Policies

| Method   | Endpoint                                          | Description                            |
|----------|---------------------------------------------------|----------------------------------------|
| `GET`    | `/gms/rest/vrf/config/securityPolicies?map=<seg>` | Get policies for a segment pair        |
| `POST`   | `/gms/rest/vrf/config/securityPolicies?map=<seg>` | Set policies for a segment pair        |

### Application Definitions

| Method   | Endpoint                                                                      | Description                            |
|----------|-------------------------------------------------------------------------------|----------------------------------------|
| `GET`    | `/gms/rest/applicationDefinition?base=<type>&resourceKey=userDefined`         | List user-defined apps by type         |
| `POST`   | `/gms/rest/applicationDefinition/portProtocolClassification?port=<p>&protocol=<n>` | Create/update port/protocol app   |
| `DELETE` | `/gms/rest/applicationDefinition/portProtocolClassification?port=<p>&protocol=<n>` | Delete port/protocol app          |
| `POST`   | `/gms/rest/applicationDefinition/dnsClassification?domain=<d>`                | Create/update DNS app                  |
| `DELETE` | `/gms/rest/applicationDefinition/dnsClassification?domain=<d>`                | Delete DNS app                         |
| `POST`   | `/gms/rest/applicationDefinition/compoundClassification?id=<id>`              | Create/update compound app             |
| `DELETE` | `/gms/rest/applicationDefinition/compoundClassification?id=<id>`              | Delete compound app                    |
| `POST`   | `/gms/rest/applicationDefinition/applications/wildcard`                       | Wildcard search across all apps        |

### Application Groups

| Method   | Endpoint                                                            | Description                        |
|----------|---------------------------------------------------------------------|------------------------------------|
| `GET`    | `/gms/rest/applicationDefinition/applicationTags?resourceKey=userDefined` | List user-defined groups      |
| `POST`   | `/gms/rest/applicationDefinition/applicationTags`                   | Set all application groups         |

### IP Objects

| Method   | Endpoint                                      | Description                  |
|----------|-----------------------------------------------|------------------------------|
| `GET`    | `/gms/rest/ipObjects/addressGroup`            | List all address groups      |
| `GET`    | `/gms/rest/ipObjects/addressGroup?name=<n>`   | Get a single address group   |
| `POST`   | `/gms/rest/ipObjects/addressGroup`            | Create/update an address group |
| `DELETE` | `/gms/rest/ipObjects/addressGroup?name=<n>`   | Delete an address group      |

### VRF Segments

| Method   | Endpoint                                  | Description                              |
|----------|-------------------------------------------|------------------------------------------|
| `GET`    | `/gms/rest/vrf/config/segments`           | List all VRF segments                    |
| `GET`    | `/gms/rest/zones/vrfSegmentZonesMap`      | List zone-to-VRF assignments (zone IDs are unique per VRF) |

### VRRP

| Method   | Endpoint                                  | Description                              |
|----------|-------------------------------------------|------------------------------------------|
| `GET`    | `/gms/rest/vrrp?nePk=<pk>&cached=<bool>`  | List the VRRP instances of one appliance |

### BGP

| Method   | Endpoint                                                        | Description                                        |
|----------|-----------------------------------------------------------------|----------------------------------------------------|
| `GET`    | `/gms/rest/bgp/config/allVrfs/system?nePk=<pk>&fromGms=<bool>`  | BGP system configuration of one appliance per VRF  |
| `GET`    | `/gms/rest/bgp/config/allVrfs/neighbor?nePk=<pk>&fromGms=<bool>`| BGP neighbors of one appliance per VRF             |
| `GET`    | `/gms/rest/bgp/config/system?nePk=<pk>&fromGms=<bool>`          | Fallback: BGP system configuration, default VRF    |
| `GET`    | `/gms/rest/bgp/config/neighbor?nePk=<pk>&fromGms=<bool>`        | Fallback: BGP neighbors, default VRF               |

Authentication is performed via the `X-Auth-Token` HTTP header containing the API key.

## Project Structure

```
terraform-provider-arubasdwan/
├── main.go                                              # Provider entry point
├── go.mod                                               # Go module definition
├── Makefile                                             # Build and install targets
├── README.md                                            # This file
├── gmsApiInfo.json                                      # Orchestrator OpenAPI spec (reference)
├── internal/
│   ├── client/
│   │   ├── client.go                                    # REST API client (zones, policies)
│   │   ├── application.go                               # REST API client (app definitions, groups)
│   │   ├── appliance.go                                 # REST API client (appliance inventory & deployment)
│   │   ├── ipobjects.go                                 # REST API client (IP address groups)
│   │   ├── vrf.go                                       # REST API client (VRF segments)
│   │   └── vrrp.go                                      # REST API client (VRRP instances)
│   └── provider/
│       ├── provider.go                                  # Provider definition & configuration
│       ├── security_zone_resource.go                    # Security zone resource (CRUD)
│       ├── security_zones_data_source.go                # Security zones data source
│       ├── security_policy_resource.go                  # Security policy resource (CRUD)
│       ├── security_policies_data_source.go             # Security policies data source
│       ├── app_port_protocol_resource.go                 # Port/protocol app definition (CRUD)
│       ├── app_port_protocols_data_source.go             # Port/protocol app definitions data source
│       ├── app_dns_classification_resource.go           # DNS app definition (CRUD)
│       ├── app_dns_classifications_data_source.go       # DNS app definitions data source
│       ├── app_compound_classification_resource.go      # Compound app definition (CRUD)
│       ├── app_compound_classifications_data_source.go  # Compound app definitions data source
│       ├── app_search_data_source.go                    # Wildcard search across all apps (built-in + user)
│       ├── application_group_resource.go                # Application group resource (CRUD)
│       ├── application_groups_data_source.go            # Application groups data source
│       ├── ip_address_group_resource.go                 # IP address group resource (CRUD)
│       ├── ip_address_groups_data_source.go             # IP address groups data source
│       ├── vrf_segments_data_source.go                  # VRF segments data source
│       ├── appliance_deployments_data_source.go         # Appliance inventory & deployment data source
│       └── vrrp_instances_data_source.go                # VRRP instances data source
└── examples/
    └── main.tf                                          # Example Terraform configuration
```

## License

This project is provided as-is for demonstration and educational purposes.
