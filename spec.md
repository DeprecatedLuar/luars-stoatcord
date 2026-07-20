# Discord → Stoat Mirror — Technical Spec

## 1. Purpose & Model

A persistent daemon that mirrors a Discord server onto a Stoat (formerly Revolt) server. **Discord is the sole source of truth.** State flows one direction only:

```
Discord  →  Canonical DB  →  Stoat
```

Stoat holds nothing of its own. Any entity that exists on Stoat without a corresponding Discord origin is reverted — deleted if it was created natively, or forced back to the mapped state if it was modified. If someone creates a channel directly on Stoat, it is reaped because it has no Discord origin.

This is a mirror, not a two-way bridge. There is no Stoat → Discord path.

### Non-goals for v1
- Two-way sync
- Threads (deferred; see §9)
- Member-level anything (structurally impossible — see §7)
- Screen-share / stage semantics beyond flattening

---

## 2. Architecture: The Canonical Spine

The core design decision. All state lives in a **neutral canonical format** in the DB. Each platform has exactly one translator pair:

- `discord → canonical` (ingest)
- `canonical → stoat` (emit)

Comparison for "is Stoat in sync?" is always done **through the canonical**: translate canonical → Stoat-shaped, compare against Stoat's actual state. **Never compare Discord-actual against Stoat-actual directly** — their permission bit layouts differ and dropped permissions would register as permanent phantom mismatches, causing endless churn.

The canonical is the referee. It is a **superset** — it models everything Discord can express, even permissions Stoat cannot represent. Loss happens *visibly* at the `canonical → stoat` step (a permission with no Stoat equivalent is a logged drop), never silently at ingest.

Benefit of the middle layer: each platform stays independent. Adding a third platform later means writing one more translator pair, not N² direct mappings.

---

## 3. Data Model

Per-entity tables. Structure entities (server, category, channel, role, emoji) are bounded and kept **forever**. Message mappings are unbounded and **pruned** (see §8).

### Identity rule
Every mapping binds on **ID, never name**. Names are user-mutable and non-unique. Name/category/position live in the row as *last-known canonical state* (for diffing) — never as the lookup key.

### Common columns (all mapping tables)
| Column | Purpose |
|---|---|
| `discord_id` | Discord snowflake — half of identity |
| `stoat_id` | Stoat ULID — half of identity; NULL while `pending` |
| `status` | `pending` \| `active` \| `deleting` — the recovery lever |
| `canonical_state` | JSON blob of last-known canonical state, for diffing |
| `created_at` / `updated_at` | bookkeeping + debug |

### Per-type tables
- **`server_map`** — name, description, icon ref, banner ref
- **`category_map`** — name, ordered `channel_ids` list (see §6 note: category is a server-level ordered list, *not* a channel field)
- **`channel_map`** — name, type, parent category, position, permission blob
- **`role_map`** — name, colour, hoist, rank, server-permission blob
- **`emoji_map`** — name, animated, nsfw
- **`message_map`** — `discord_msg_id`, `stoat_msg_id`, channel ref, `created_at` (pruned; see §8)
- **`channel_cursor`** — one row per channel: `channel_id → last_synced_discord_msg_id`. **Kept separately from `message_map`** so it survives pruning and continues to drive backfill.

### Permission representation
Permissions are stored **per channel** as a JSON blob keyed by role, tri-state:

```json
{
  "<canonical_role_id>": { "allow": ["VIEW_CHANNEL", "SEND_MESSAGES"], "deny": ["ADD_REACTIONS"] }
}
```

Tri-state (allow / deny / inherit) is mandatory — a role can be explicitly denied, explicitly allowed, or neither. Storing only "allowed" cannot represent an explicit deny and would mistranslate. This maps directly to Stoat's `PermissionsOverwrite(allow, deny)` and to Discord's overwrite model.

Blob-per-channel (rather than a separate per-(channel,role) table) is chosen deliberately: the whole permission snapshot is compared in one shot against the translated-Stoat snapshot, no per-row bookkeeping or multi-row atomicity concerns.

---

## 4. Canonical Permission Vocabulary

The canonical permission set is the **union of Discord's set**, with each entry mapped to its Stoat equivalent or explicitly marked as a drop.

> Reconciled against the live Stoat `Permission` enum (`crates/core/permissions/src/models/channel.rs`, `stoatchat/stoatchat`) in Phase 0. All names below are confirmed exact.

| Canonical | Discord | Stoat | Notes |
|---|---|---|---|
| `VIEW_CHANNEL` | View Channel | `ViewChannel` | |
| `READ_HISTORY` | Read Message History | `ReadMessageHistory` | |
| `SEND_MESSAGES` | Send Messages | `SendMessage` | |
| `MANAGE_MESSAGES` | Manage Messages | `ManageMessages` | |
| `EMBED_LINKS` | Embed Links | `SendEmbeds` | |
| `ATTACH_FILES` | Attach Files | `UploadFiles` | |
| `ADD_REACTIONS` | Add Reactions | `React` | |
| `MANAGE_CHANNEL` | Manage Channels | `ManageChannel` | |
| `MANAGE_SERVER` | Manage Server | `ManageServer` | |
| `MANAGE_ROLES` | Manage Roles/Permissions | `ManagePermissions` | |
| `CREATE_INVITE` | Create Invite | `InviteOthers` | |
| `CONNECT` | Connect (voice) | `Connect` | |
| `SPEAK` | Speak (voice) | `Speak` | |
| `VIDEO` | Video (voice) | `Video` | |
| `MUTE_MEMBERS` | Mute Members | `MuteMembers` | |
| `DEAFEN_MEMBERS` | Deafen Members | `DeafenMembers` | |
| `MOVE_MEMBERS` | Move Members | `MoveMembers` | |
| `MANAGE_WEBHOOKS` | Manage Webhooks | `ManageWebhooks` | approximate |
| `MANAGE_EMOJI` | Manage Expressions | `ManageCustomisation` | approximate |
| `MANAGE_NICKNAMES` | Manage Nicknames | `ManageNicknames` | |
| `KICK_MEMBERS` | Kick Members | `KickMembers` | |
| `BAN_MEMBERS` | Ban Members | `BanMembers` | |
| `TIMEOUT_MEMBERS` | Timeout Members | `TimeoutMembers` | |
| `MENTION_EVERYONE` | Mention @everyone | `MentionEveryone` | |
| `VIEW_AUDIT_LOG` | View Audit Log | — | **DROP** (see below) |
| `USE_APP_COMMANDS` | Use Application Commands | — | **DROP** |
| `PRIORITY_SPEAKER` | Priority Speaker | — | **DROP** |
| `SEND_TTS` | Send TTS Messages | — | **DROP** |

Any Discord permission not in the table is treated as an explicit **drop** and logged — never a silent loss.

`VIEW_AUDIT_LOG` is a special case: Stoat's enum does define a real bit for it (`ViewAuditLogs`, `1<<40`), but Stoat's own web client (`stoatchat/for-web`, `ChannelPermissionsEditor.tsx`) has no UI toggle for it at all -- it is the only permission in this table with no path to grant it through normal server administration. The only way to set it on any role is a raw API call from the server owner's own token (owner calls bypass the "can't grant what you don't have" check entirely, `crates/core/permissions/src/impl.rs`), which the daemon never holds and has no reason to. Treated as unreachable in practice and dropped, same as a true no-equivalent permission.

---

## 5. Sync Engine

### Event-driven, no timers for the live path
Discord Gateway events (`CHANNEL_CREATE/UPDATE/DELETE`, role changes, permission-overwrite changes, message create/edit/delete) drive live sync directly. No polling.

### Per-operation flow
Each event resolves to a canonical operation. Before any **mutating** operation:

1. **Pre-flight health check.** Ping both gateways (Discord + Stoat). Both green → proceed. Either degraded → do not mutate; enqueue (see §8).
2. **Diff against DB.** Translate canonical → Stoat-shaped, compare against the stored last-known-Stoat snapshot. If already matched, no-op (idempotent). If mismatched, act.
3. **Write intent, then remote, then confirm** (see §8 recovery).

The health check is a per-operation gate, **not** a heartbeat subsystem. It guards writes. It does *not* recover missed events — that is reconciliation's job (§8).

### Trust boundary
- **Live path trusts the stored DB snapshot** (no extra read of Stoat's current state per op — avoids rate-limit pressure).
- **Reconcile path re-reads Stoat truth** and corrects drift.

### Dependency ordering
Entities are not independent. Operations must respect:
- channel create requires its category to exist
- a role's channel-permission overwrite requires both the role and channel to exist
- a message referencing a custom emoji requires that emoji created first
- a reply requires the parent message already mapped

The operation layer must support **defer-and-retry**: if a dependency mapping isn't ready, hold the op and retry once the dependency lands. This applies to both live sync (events arrive out of order) and reconciliation (Stoat must be built in dependency order, not flat iteration).

### Per-channel message ordering
Stoat assigns message time from the ULID at creation, so send-order = display-order. Delivery must be **sequential within a channel**. Across channels, parallel is fine.

### Rate limiting
The Stoat write side needs a throttle, especially the initial backfill burst (history + attachment re-uploads). Stoat's API returns rate-limit errors; the writer must back off and respect them.

---

## 6. Entity Handling Matrix

| Discord | Stoat | Handling |
|---|---|---|
| Text channel | `text_channel` | 1:1 |
| Voice channel | `voice_channel` | 1:1 |
| Announcement channel | `text_channel` | flatten — lose follow/publish semantics, keep messages |
| Stage channel | `voice_channel` | flatten |
| Forum channel | `text_channel` | flatten — forum-of-threads collapses to flat channel |
| Thread | — | **deferred to post-v1** (§9) |
| Role | `Role` | 1:1 (name, colour, hoist, rank, permissions) |
| Channel perm (role-level) | `set_role_permissions` | 1:1 via canonical |
| Channel perm (member-level) | — | **impossible** (§7) |
| Category | server-level ordered list | see note below |
| Category-level perms | — | **expand** onto each child channel individually |
| Custom emoji | `create_emoji` | auto-create on first use |
| Server metadata | name/desc/icon/banner | editable, synced |

### Category note
Stoat's `Category` lives on the **server** and holds an ordered `channel_ids` array. A Discord channel has `parent_id` pointing *up*; Stoat does not. Therefore:
- "Move channel X into category Y" = mutate the server's category lists (remove X's id from one array, add to another). **Not** a channel edit.
- Sidebar reordering = same server-level ordered-list mutation.
- The canonical must model category as a **server-level structural operation**, not a field on the channel.

### Category permissions
Stoat categories have no permission concept. A Discord category-level permission change is **expanded**: apply those permissions to each child channel individually at sync time.

---

## 7. Identity & Echo Prevention

### Masquerade — no real Stoat accounts
All mirrored messages are posted by the single bot account using Stoat's native `masquerade` field (name / avatar / colour override, per-message). No Discord user needs a linked Stoat account.

**Consequence (positive):** every bridged message is *owned by the bot*. Stoat allows a bot to edit/delete its own messages, so edit and delete propagation works with no permission wall.

**Consequence (structural):** there is no per-user Stoat identity, so Discord **member-level** permission overwrites have no target entity. They are not "cut from scope" — they are a non-concept in this architecture. Role-level overwrites sync; member-level is silently dropped (logged).

### Echo / self-loop prevention — CRITICAL
The bot must listen to Stoat (to enforce "reap anything not from Discord"). But the bot also *writes* to Stoat. Its own writes fire Stoat events indistinguishable from a human's.

Without a guard: bot creates channel on Stoat → Stoat fires `channel_create` → bot's watcher sees an unmapped-looking channel → reaps the channel it just made. Loop.

**The guard:** before reaping *any* Stoat entity, look it up in the mapping table by `stoat_id`.
- Has a row (`pending` or `active`) → it's ours, leave it.
- No row → foreign, reap it.

This lives in the **reaper path**, distinct from the Discord→Stoat writer path. The DB check the writer does on ingest does not cover this; the reaper needs its own lookup.

### Reaper contract
- **Scope:** the mirrored Stoat server only. If the bot account is ever in another Stoat server, the reaper must never touch it.
- **Unmapped create** → delete.
- **Unmapped edit** (someone renames a mirrored channel on Stoat) → force back to mapped state by pushing the stored canonical last-known-state. The reaper is a *state enforcer*, not just a deleter. It diffs against the stored snapshot and corrects — it does **not** re-derive live from Discord on every reap.

---

## 8. Recovery, Reconciliation & Retention

### Partial-write recovery (the `pending` pattern)
No true atomicity exists across DB + remote API. The defended failure is the partial write: create on Stoat, crash before recording the mapping → orphan + no record → next reconcile creates a duplicate.

Ordering fixes it:
1. Write intent row: `discord_id`, target state, `status = pending`, `stoat_id = NULL`.
2. Call Stoat API to create.
3. Update row with returned `stoat_id`, `status = active`.

Crash between 2 and 3 → on restart the `pending` row says "I was mid-creating this; check Stoat before retrying," and the orphan gets adopted rather than duplicated.

### Reconcile-before-reap (ordering is mandatory)
On startup:
1. **Reconcile pending rows first** — adopt orphans, complete mappings.
2. **Then run the reaper.**

If the reaper runs first, it sees the unmapped orphan (whose `stoat_id` was never written) and deletes the bot's own legitimate work. Reconcile must win the race.

### Reconciliation pass (gap recovery)
The health check guards writes but cannot recover events missed while offline. On reconnect / startup:
- **Structure:** full-state diff (Stoat actual vs canonical desired), patch drift, in dependency order.
- **Messages:** per-channel cursor drives `after = last_synced_discord_msg_id` — cheap, bounded, idempotent.

Degraded-window events are queued **durably** (disk-backed, not in-memory) and drained in order on recovery. Same subsystem handles "briefly degraded" and "offline for an hour" — only queue depth differs.

### Message retention
Message-mapping rows are ~100–150 bytes. Even a busy server (10k msgs/day) is ~45 MB/month — storage is not the constraint. The real constraint is how stale an edit you want to honor; Discord's own bulk-delete only reaches 14 days.

- **Retention window: 30 days.** Prune older `message_map` rows.
- Widen later (90d) with zero consequence if ever desired.
- **`channel_cursor` is never pruned** — it outlives individual message rows and keeps backfill working.
- Do **not** derive the cursor from `max(message_map.id)` — that vanishes on prune.

### Deletes — the asymmetric-danger direction
Every other mistake self-heals (a duplicate or stale name corrects on next sync). A wrong delete is gone, no undo.

- Pre-flight both-green ping matters **most** on delete ops.
- **Decision flagged:** consider a short debounce on deletes — wait a few seconds, confirm the entity is really gone from Discord (not a transient gateway drop misread as deletion), then propagate. This is the one place a small deliberate delay is a safety feature, not a timer-hack. **Open decision: instant vs. debounced delete.**

### Message pipeline details
- **Attachments:** Discord CDN links expire → download from Discord, re-upload to Stoat's file service (Autumn, tag `attachments`), store the mapping so later edits/deletes resolve.
- **Backfill timestamps:** Stoat assigns message time from the ULID at send; there is **no** way to back-date a message. Backfilled history shows *bridged-time*, not original Discord time. **Decision: accepted — do not fake original time.**
- **Reactions:** syncable, but the emoji must exist first, and reaction-author identity is lost (the bot reacts, not the original user). Minor.

---

## 9. Deferred / Open

### Deferred to post-v1
- **Threads** — Stoat has no thread concept. Handling (flatten into parent with a prefix, vs. drop) is undecided and skipped for v1.

### Open decisions
1. **Delete propagation:** instant vs. short debounce (§8).

### Verify against live API before coding
1. **Permission vocabulary (§4)** — reconcile exact Stoat `Permission` enum bit names.
2. Confirm masquerade survives a message *edit* (expected yes, since masquerade is stored on the message).
3. Confirm Autumn upload tags / limits for attachment re-upload.

---

## 10. Decision Summary (quick reference)

| Area | Decision |
|---|---|
| Direction | One-way, Discord = truth |
| Middle layer | Canonical DB, one translator pair per platform |
| Comparison | Always through canonical; never Discord-actual vs Stoat-actual |
| Identity | Bind on ID, never name |
| Author model | Masquerade, no real Stoat accounts |
| Member-level perms | Structurally impossible — non-concept |
| Category | Server-level ordered list, not a channel field |
| Category perms | Expand onto each child channel |
| Recovery | `pending` row before remote call |
| Startup order | Reconcile-before-reap (mandatory) |
| Echo guard | Reaper checks mapping table by `stoat_id` |
| Reaper scope | Mirrored server only |
| Live vs reconcile | Live trusts DB snapshot; reconcile re-reads truth |
| Msg retention | 30 days; cursor kept separately, never pruned |
| Backfill time | Bridged-time, no faked timestamps |
| Threads | Deferred |
