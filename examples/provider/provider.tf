provider "arubasdwan" {
  orchestrator_url = "https://orchestrator.example.com"
  api_key          = var.orch_api_key
  insecure         = true
}
