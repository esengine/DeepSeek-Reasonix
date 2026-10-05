import type { EventRow } from "../types";

export type EventType = "publish" | "update" | "install" | "star" | "milestone";

export interface NewEvent {
  type: EventType;
  packageId: number | null;
  actorHandle: string;
  summary: string;
  now: string;
}

export class EventRepo {
  constructor(private readonly db: D1Database) {}

  async log(e: NewEvent): Promise<void> {
    await this.db
      .prepare(
        `INSERT INTO events (type, package_id, actor_handle, summary, created_at)
         VALUES (?1, ?2, ?3, ?4, ?5)`,
      )
      .bind(e.type, e.packageId, e.actorHandle, e.summary.slice(0, 200), e.now)
      .run();
  }

  // Most-recent social activity, joined to the package slug so the feed can link
  // back. Raw 'install' pings are excluded — they exist only to feed the trending
  // rank; the feed surfaces publish/update/star and install-milestone events.
  async recent(limit: number): Promise<EventRow[]> {
    const res = await this.db
      .prepare(
        `SELECT e.type AS type, p.slug AS slug, e.actor_handle AS actor_handle,
                e.summary AS summary, e.created_at AS created_at
         FROM events e
         LEFT JOIN packages p ON p.id = e.package_id
         WHERE e.type IN ('publish', 'update', 'star', 'milestone')
         ORDER BY e.created_at DESC
         LIMIT ?1`,
      )
      .bind(limit)
      .all<EventRow>();
    return res.results ?? [];
  }
}
