package main

import "reasonix/internal/provider"

func historyPersistedUserRole(role provider.Role, pinnedRevision bool) provider.Role {
	if pinnedRevision && role == provider.RoleUser {
		return ""
	}
	return role
}
