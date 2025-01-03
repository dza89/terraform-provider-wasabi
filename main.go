package main

// Run "go generate" to format example terraform files and generate the docs for the registry/website

// Run the docs generation tool, check its repository for more information on how it works and how docs
// can be customized.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
	"github.com/dza89/terraform-provider-wasabi/aws"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: aws.Provider})
}
