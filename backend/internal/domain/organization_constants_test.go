package domain

import "testing"

func TestOrganizationRoleConstantsRemainDistinctFromPlatformRoles(t *testing.T) {
	if OrganizationRoleAdmin != "organization_admin" {
		t.Fatalf("OrganizationRoleAdmin = %q", OrganizationRoleAdmin)
	}
	if OrganizationRoleMember != "organization_member" {
		t.Fatalf("OrganizationRoleMember = %q", OrganizationRoleMember)
	}
	if OrganizationRoleAdmin == RoleAdmin || OrganizationRoleMember == RoleUser {
		t.Fatal("organization roles must not reuse platform role values")
	}
}
