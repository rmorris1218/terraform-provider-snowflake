package resources

import (
	"errors"

	"github.com/Snowflake-Labs/terraform-provider-snowflake/pkg/snowflake"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

/*
NewPriviligeSet creates a set of privileges that are allowed
They are used for validation in the schema object below.
*/

var validMaterializedViewPrivileges = NewPrivilegeSet(
	privilegeOwnership,
	privilegeReferences,
	privilegeSelect,
)

// The schema holds the resource variables that can be provided in the Terraform.
var materializedViewGrantSchema = map[string]*schema.Schema{
	"materialized_view_name": {
		Type:        schema.TypeString,
		Optional:    true,
		Description: "The name of the materialized view on which to grant privileges immediately (only valid if on_future is false).",
		ForceNew:    true,
	},
	"schema_name": {
		Type:        schema.TypeString,
		Optional:    true,
		Description: "The name of the schema containing the current or future materialized views on which to grant privileges.",
		ForceNew:    true,
	},
	"database_name": {
		Type:        schema.TypeString,
		Required:    true,
		Description: "The name of the database containing the current or future materialized views on which to grant privileges.",
		ForceNew:    true,
	},
	"privilege": {
		Type:         schema.TypeString,
		Optional:     true,
		Description:  "The privilege to grant on the current or future materialized view view.",
		Default:      "SELECT",
		ValidateFunc: validation.StringInSlice(validMaterializedViewPrivileges.ToList(), true),
		ForceNew:     true,
	},
	"roles": {
		Type:        schema.TypeSet,
		Elem:        &schema.Schema{Type: schema.TypeString},
		Optional:    true,
		Description: "Grants privilege to these roles.",
	},
	"shares": {
		Type:        schema.TypeSet,
		Elem:        &schema.Schema{Type: schema.TypeString},
		Optional:    true,
		Description: "Grants privilege to these shares (only valid if on_future is false).",
	},
	"on_future": {
		Type:        schema.TypeBool,
		Optional:    true,
		Description: "When this is set to true and a schema_name is provided, apply this grant on all future materialized views in the given schema. When this is true and no schema_name is provided apply this grant on all future materialized views in the given database. The materialized_view_name and shares fields must be unset in order to use on_future.",
		Default:     false,
		ForceNew:    true,
	},
	"with_grant_option": {
		Type:        schema.TypeBool,
		Optional:    true,
		Description: "When this is set to true, allows the recipient role to grant the privileges to other roles.",
		Default:     false,
		ForceNew:    true,
	},
	"enable_multiple_grants": {
		Type:        schema.TypeBool,
		Optional:    true,
		Description: "When this is set to true, multiple grants of the same type can be created. This will cause Terraform to not revoke grants applied to roles and objects outside Terraform.",
		Default:     false,
		ForceNew:    true,
	},
}

// ViewGrant returns a pointer to the resource representing a view grant.
func MaterializedViewGrant() *TerraformGrantResource {
	return &TerraformGrantResource{
		Resource: &schema.Resource{
			Create: CreateMaterializedViewGrant,
			Read:   ReadMaterializedViewGrant,
			Delete: DeleteMaterializedViewGrant,
			Update: UpdateMaterializedViewGrant,

			Schema: materializedViewGrantSchema,
			Importer: &schema.ResourceImporter{
				StateContext: schema.ImportStatePassthroughContext,
			},
		},
		ValidPrivs: validMaterializedViewPrivileges,
	}
}

// CreateViewGrant implements schema.CreateFunc.
func CreateMaterializedViewGrant(d *schema.ResourceData, meta interface{}) error {
	var materializedViewName string
	if name, ok := d.GetOk("materialized_view_name"); ok {
		materializedViewName = name.(string)
	}
	dbName := d.Get("database_name").(string)
	schemaName := d.Get("schema_name").(string)
	priv := d.Get("privilege").(string)
	futureMaterializedViews := d.Get("on_future").(bool)
	grantOption := d.Get("with_grant_option").(bool)
	roles := expandStringList(d.Get("roles").(*schema.Set).List())

	if (schemaName == "") && !futureMaterializedViews {
		return errors.New("schema_name must be set unless on_future is true")
	}

	if (materializedViewName == "") && !futureMaterializedViews {
		return errors.New("materialized_view_name must be set unless on_future is true")
	}
	if (materializedViewName != "") && futureMaterializedViews {
		return errors.New("materialized_view_name must be empty if on_future is true")
	}

	var builder snowflake.GrantBuilder
	if futureMaterializedViews {
		builder = snowflake.FutureMaterializedViewGrant(dbName, schemaName)
	} else {
		builder = snowflake.MaterializedViewGrant(dbName, schemaName, materializedViewName)
	}

	if err := createGenericGrant(d, meta, builder); err != nil {
		return err
	}

	grant := &grantID{
		ResourceName: dbName,
		SchemaName:   schemaName,
		ObjectName:   materializedViewName,
		Privilege:    priv,
		GrantOption:  grantOption,
		Roles:        roles,
	}
	dataIDInput, err := grant.String()
	if err != nil {
		return err
	}
	d.SetId(dataIDInput)

	return ReadMaterializedViewGrant(d, meta)
}

// ReadViewGrant implements schema.ReadFunc.
func ReadMaterializedViewGrant(d *schema.ResourceData, meta interface{}) error {
	grantID, err := grantIDFromString(d.Id())
	if err != nil {
		return err
	}
	dbName := grantID.ResourceName
	schemaName := grantID.SchemaName
	materializedViewName := grantID.ObjectName
	priv := grantID.Privilege

	if err := d.Set("database_name", dbName); err != nil {
		return err
	}
	if err := d.Set("schema_name", schemaName); err != nil {
		return err
	}
	futureMaterializedViewsEnabled := false
	if materializedViewName == "" {
		futureMaterializedViewsEnabled = true
	}
	if err := d.Set("materialized_view_name", materializedViewName); err != nil {
		return err
	}
	if err := d.Set("on_future", futureMaterializedViewsEnabled); err != nil {
		return err
	}
	if err := d.Set("privilege", priv); err != nil {
		return err
	}
	if err := d.Set("with_grant_option", grantID.GrantOption); err != nil {
		return err
	}

	var builder snowflake.GrantBuilder
	if futureMaterializedViewsEnabled {
		builder = snowflake.FutureMaterializedViewGrant(dbName, schemaName)
	} else {
		builder = snowflake.MaterializedViewGrant(dbName, schemaName, materializedViewName)
	}

	return readGenericGrant(d, meta, materializedViewGrantSchema, builder, futureMaterializedViewsEnabled, validMaterializedViewPrivileges)
}

// DeleteViewGrant implements schema.DeleteFunc.
func DeleteMaterializedViewGrant(d *schema.ResourceData, meta interface{}) error {
	grantID, err := grantIDFromString(d.Id())
	if err != nil {
		return err
	}
	dbName := grantID.ResourceName
	schemaName := grantID.SchemaName
	materializedViewName := grantID.ObjectName

	futureMaterializedViews := (materializedViewName == "")

	var builder snowflake.GrantBuilder
	if futureMaterializedViews {
		builder = snowflake.FutureMaterializedViewGrant(dbName, schemaName)
	} else {
		builder = snowflake.MaterializedViewGrant(dbName, schemaName, materializedViewName)
	}
	return deleteGenericGrant(d, meta, builder)
}

// UpdateMaterializedViewGrant implements schema.UpdateFunc.
func UpdateMaterializedViewGrant(d *schema.ResourceData, meta interface{}) error {
	// for now the only thing we can update are roles or shares
	// if nothing changed, nothing to update and we're done
	if !d.HasChanges("roles", "shares") {
		return nil
	}

	rolesToAdd := []string{}
	rolesToRevoke := []string{}
	sharesToAdd := []string{}
	sharesToRevoke := []string{}
	if d.HasChange("roles") {
		rolesToAdd, rolesToRevoke = changeDiff(d, "roles")
	}
	if d.HasChange("shares") {
		sharesToAdd, sharesToRevoke = changeDiff(d, "shares")
	}
	grantID, err := grantIDFromString(d.Id())
	if err != nil {
		return err
	}

	dbName := grantID.ResourceName
	schemaName := grantID.SchemaName
	materializedViewName := grantID.ObjectName
	futureMaterializedViews := (materializedViewName == "")

	// create the builder
	var builder snowflake.GrantBuilder
	if futureMaterializedViews {
		builder = snowflake.FutureMaterializedViewGrant(dbName, schemaName)
	} else {
		builder = snowflake.MaterializedViewGrant(dbName, schemaName, materializedViewName)
	}

	// first revoke
	if err := deleteGenericGrantRolesAndShares(
		meta, builder, grantID.Privilege, rolesToRevoke, sharesToRevoke,
	); err != nil {
		return err
	}
	// then add
	if err := createGenericGrantRolesAndShares(
		meta, builder, grantID.Privilege, grantID.GrantOption, rolesToAdd, sharesToAdd,
	); err != nil {
		return err
	}

	// Done, refresh state
	return ReadMaterializedViewGrant(d, meta)
}
