---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "wasabi_policy_document Data Source - terraform-provider-wasabi"
subcategory: ""
description: |-
  
---

# wasabi_policy_document (Data Source)





<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `id` (String) The ID of this resource.
- `override_json` (String)
- `policy_id` (String)
- `source_json` (String)
- `statement` (Block List) (see [below for nested schema](#nestedblock--statement))
- `version` (String)

### Read-Only

- `json` (String)

<a id="nestedblock--statement"></a>
### Nested Schema for `statement`

Optional:

- `actions` (Set of String)
- `condition` (Block Set) (see [below for nested schema](#nestedblock--statement--condition))
- `effect` (String)
- `not_actions` (Set of String)
- `not_principals` (Block Set) (see [below for nested schema](#nestedblock--statement--not_principals))
- `not_resources` (Set of String)
- `principals` (Block Set) (see [below for nested schema](#nestedblock--statement--principals))
- `resources` (Set of String)
- `sid` (String)

<a id="nestedblock--statement--condition"></a>
### Nested Schema for `statement.condition`

Required:

- `test` (String)
- `values` (Set of String)
- `variable` (String)


<a id="nestedblock--statement--not_principals"></a>
### Nested Schema for `statement.not_principals`

Required:

- `identifiers` (Set of String)
- `type` (String)


<a id="nestedblock--statement--principals"></a>
### Nested Schema for `statement.principals`

Required:

- `identifiers` (Set of String)
- `type` (String)

