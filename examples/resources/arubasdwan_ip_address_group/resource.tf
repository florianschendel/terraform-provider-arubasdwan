resource "arubasdwan_ip_address_group" "example" {
  name = "OfficeSubnets"
  rules = [
    {
      included_ips    = ["192.168.1.0/24", "192.168.2.0/24"]
      excluded_ips    = ["192.168.1.5/32"]
      included_groups = []
      comment         = "Main office subnets"
    },
  ]
}
