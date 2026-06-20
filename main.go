// Package main is the entry point for the Aruba SD-WAN Terraform Provider.
//
// This provider enables Terraform to manage resources on an Aruba EdgeConnect
// SD-WAN Orchestrator, including security zones, firewall policies, application
// classifications (port/protocol, DNS, compound), and
// application groups.
//
// The provider binary is served as a gRPC plugin that Terraform core communicates
// with using the Terraform Plugin Protocol. It supports both normal operation and
// a debug mode for attaching a debugger (e.g. Delve) during development.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/florianschendel/terraform-provider-arubasdwan/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is set at build time via ldflags (e.g. -ldflags "-X main.version=1.0.0").
// It defaults to "dev" for local development builds.
var (
	version string = "dev"
)

func main() {
	// The --debug flag enables the provider to start in debug mode, which pauses
	// execution until a debugger attaches. This is useful during development with
	// tools like Delve (dlv).
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	// ServeOpts configures how the provider is served to Terraform.
	// The Address must match the provider source address used in Terraform
	// configuration blocks (e.g. required_providers { arubasdwan = { source = "..." } }).
	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/florianschendel/arubasdwan",
		Debug:   debug,
	}

	// Serve starts the gRPC server that Terraform core connects to.
	// provider.New(version) returns a factory function that creates a new provider
	// instance for each Terraform operation. The version string is passed through
	// so it can be reported in provider metadata.
	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
