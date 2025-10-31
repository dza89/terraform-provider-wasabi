# Terraform Provider for Wasabi

This is a fork of the official AWS provider, with everything not relevant for
Wasabi removed. For personal use, pretty much work in progress, likely with many
bugs, will fix them as I face them.

## Development

### Building from Source

To build the provider from source:

```bash
git clone https://github.com/angelbarrera92/terraform-provider-wasabi.git
cd terraform-provider-wasabi
go build -o terraform-provider-wasabi .
```

### Testing Local Builds

To test your local build without installing it system-wide, you can use Terraform's development overrides feature.

#### Step 1: Create a `.terraformrc` file

Create or edit `~/.terraformrc` with the following content:

```hcl
provider_installation {
  # Override the wasabi provider with your local build
  dev_overrides {
    "registry.terraform.io/dza89/wasabi" = "/path/to/your/terraform-provider-wasabi"
  }

  # For all other providers, use the default registry
  direct {}
}
```

**Note:** Replace `/path/to/your/terraform-provider-wasabi` with the actual path to your cloned repository.

#### Step 2: Test Your Configuration

In your Terraform project directory:

```bash
# Clean up any cached provider data
rm -rf .terraform/
rm -f .terraform.lock.hcl

# Verify the override is active (you should see a warning about dev overrides)
terraform version

# Initialize and test
terraform init
terraform plan
```

When the development override is active, Terraform will show a warning:
```
Warning: Provider development overrides are in effect
```

#### Step 3: Disable Override (When Done Testing)

To return to using the registry version, rename or remove the `.terraformrc` file:

```bash
mv ~/.terraformrc ~/.terraformrc.backup
```
