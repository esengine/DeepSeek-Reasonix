export type Role = "member" | "admin";
export type UserStatus = "active" | "suspended" | "deleted";

// A full `users` row as stored in D1.
export interface UserRow {
  id: number;
  handle: string;
  email: string;
  email_verified: number;
  password_hash: string | null;
  display_name: string;
  avatar_url: string;
  bio: string;
  role: Role;
  status: UserStatus;
  created_at: string;
  updated_at: string;
}

// The authenticated owner's view of their own account (login, /me). Camel-cased
// and free of the password hash, so it is safe to serialize directly.
export interface AccountUser {
  id: number;
  handle: string;
  email: string;
  emailVerified: boolean;
  displayName: string;
  avatarUrl: string;
  bio: string;
  role: Role;
  status: UserStatus;
  createdAt: string;
}

// What anyone may see at /u/<handle>. No email, no role, no status.
export interface PublicUser {
  handle: string;
  displayName: string;
  avatarUrl: string;
  bio: string;
  joinedAt: string;
}

export function toAccountUser(row: UserRow): AccountUser {
  return {
    id: row.id,
    handle: row.handle,
    email: row.email,
    emailVerified: row.email_verified === 1,
    displayName: row.display_name,
    avatarUrl: row.avatar_url,
    bio: row.bio,
    role: row.role,
    status: row.status,
    createdAt: row.created_at,
  };
}

export function toPublicUser(row: UserRow): PublicUser {
  return {
    handle: row.handle,
    displayName: row.display_name || row.handle,
    avatarUrl: row.avatar_url,
    bio: row.bio,
    joinedAt: row.created_at,
  };
}
