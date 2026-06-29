import type { Role, UserRow } from "../types";

export interface NewUser {
  handle: string;
  email: string; // already lowercased
  passwordHash: string;
  displayName: string;
  role: Role;
}

export interface ProfilePatch {
  handle?: string;
  displayName?: string;
  bio?: string;
  avatarUrl?: string;
}

export class UserRepo {
  constructor(private readonly db: D1Database) {}

  byId(id: number): Promise<UserRow | null> {
    return this.db.prepare("SELECT * FROM users WHERE id = ?1").bind(id).first<UserRow>();
  }

  byEmail(email: string): Promise<UserRow | null> {
    return this.db.prepare("SELECT * FROM users WHERE email = ?1").bind(email).first<UserRow>();
  }

  byHandle(handle: string): Promise<UserRow | null> {
    return this.db.prepare("SELECT * FROM users WHERE handle = ?1").bind(handle).first<UserRow>();
  }

  async handleTaken(handle: string): Promise<boolean> {
    const row = await this.db.prepare("SELECT 1 AS x FROM users WHERE handle = ?1 LIMIT 1").bind(handle).first<{ x: number }>();
    return row !== null;
  }

  async create(input: NewUser): Promise<UserRow> {
    const now = new Date().toISOString();
    const res = await this.db
      .prepare(
        `INSERT INTO users (handle, email, email_verified, password_hash, display_name, avatar_url, bio, role, status, created_at, updated_at)
         VALUES (?1, ?2, 0, ?3, ?4, '', '', ?5, 'active', ?6, ?6)`,
      )
      .bind(input.handle, input.email, input.passwordHash, input.displayName, input.role, now)
      .run();
    const row = await this.byId(Number(res.meta.last_row_id));
    if (!row) throw new Error("user row missing right after insert");
    return row;
  }

  async markEmailVerified(id: number): Promise<void> {
    await this.db
      .prepare("UPDATE users SET email_verified = 1, updated_at = ?2 WHERE id = ?1")
      .bind(id, new Date().toISOString())
      .run();
  }

  async updatePassword(id: number, passwordHash: string): Promise<void> {
    await this.db
      .prepare("UPDATE users SET password_hash = ?2, updated_at = ?3 WHERE id = ?1")
      .bind(id, passwordHash, new Date().toISOString())
      .run();
  }

  async updateProfile(id: number, patch: ProfilePatch): Promise<UserRow> {
    const sets: string[] = [];
    const binds: unknown[] = [];
    const add = (col: string, value: unknown): void => {
      binds.push(value);
      sets.push(`${col} = ?${binds.length}`);
    };
    if (patch.handle !== undefined) add("handle", patch.handle);
    if (patch.displayName !== undefined) add("display_name", patch.displayName);
    if (patch.bio !== undefined) add("bio", patch.bio);
    if (patch.avatarUrl !== undefined) add("avatar_url", patch.avatarUrl);
    add("updated_at", new Date().toISOString());
    binds.push(id);
    await this.db.prepare(`UPDATE users SET ${sets.join(", ")} WHERE id = ?${binds.length}`).bind(...binds).run();
    const row = await this.byId(id);
    if (!row) throw new Error("user row missing after profile update");
    return row;
  }

  async softDelete(id: number): Promise<void> {
    await this.db
      .prepare("UPDATE users SET status = 'deleted', updated_at = ?2 WHERE id = ?1")
      .bind(id, new Date().toISOString())
      .run();
  }
}
