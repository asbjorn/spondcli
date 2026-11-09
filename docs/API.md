# Spond API

Reverse engineered from the Android app `com.spond.spond` **4.64.1 (build 2924)**,
decompiled with jadx 1.5.1. The app is native Kotlin using a Ktor HTTP client and
kotlinx.serialization. Network code lives in `com.spond.sdk.data.network.api.*`.

Nothing here is an official, documented API. Field lists come from the
kotlinx.serialization descriptors; anything marked *unverified* was inferred.

## Base URL

```
https://api.spond.com/core/v1/
```

Source: `com.spond.sdk.config.ServerConfig.productServer` (`https://api.spond.com/`)
plus `Server.corePath = "core"`. The app also reports `SERVER_API_VERSION = "2.7.16"`.

## Authentication

### Login

```
POST core/v1/auth2/login
Content-Type: application/json
X-Spond-Timezone: Europe/Oslo
```

Body (`ApiLoginRequest`) — exactly one of `email` / `phoneNumber`:

```json
{ "email": "user@example.com", "password": "secret" }
```

The response (`ApiLoginResponseSerializer`) is one of two shapes, chosen by key:

**Tokens** (contains `accessToken`) — `ApiLoginTokens`:

```json
{
  "accessToken":   { "token": "…", "expiration": "2026-10-23T08:00:00Z" },
  "refreshToken":  { "token": "…", "expiration": "…" },
  "passwordToken": { "token": "…", "expiration": "…" }
}
```

**Two-factor challenge** (contains `phoneNumber`) — `Api2faLoginResponse`:

```json
{ "phoneNumber": "+47…", "token": "…" }
```

### Two-factor verification

```
POST core/v1/auth2/2fa/verify
{ "token": "<token from 2FA challenge>", "code": "123456" }
```

Returns `ApiLoginTokens` (same shape as above).

Related: `core/v1/auth2/2fa/resend`, `core/v1/auth2/logout` and
`core/v1/auth2/login/refresh`; see [Account](#account-authapi).

### Authorizing requests

Every authenticated call sends (`CoreAuthProvider`):

```
Authorization: Bearer <accessToken.token>
```

## Groups (`GroupApi`)

### List groups

```
GET core/v1/groups?includeMembers=true
```

| Param            | Type | Notes                                      |
|------------------|------|--------------------------------------------|
| `includeMembers` | bool | Default `true`. `false` gives a lighter response. |

Response: JSON array of `ApiGroup`. Fields:

`id`, `type`, `organizationType`, `name`, `description`, `welcomeMessage`,
`imageUrl`, `activity`, `signupUrl`, `signupCode`, `countryCode`, `clubId`,
`clubName`, `clubLogo`, `club`, `chatAgeLimit`, `createdTime`, `primaryContact`,
`eventVisibility`, `clientOutOfDate`, `allowSmsNag`, `bonusEnabled`,
`contactInfoHidden`, `shareContactInfo`, `allowPrivatePayoutAccounts`,
`adminsCanAddMembers`, `contactPerson`, `memberPermissions`,
`guardianPermissions`, `addressFormat`, `defaultFields`, `fieldDefs`,
`payoutAccounts`, `membershipRequests`, `members`, `myMembership`,
`behalfOfMemberships`, `subGroups`, `roles`.

`subGroups[]` (`ApiSubgroup`): `id`, `name`, `color`, `imageUrl`.

`members[]` (`ApiMembership`): `id`, `clubMembershipId`, `profile` (`{ id, firstName,
lastName, imageUrl, contactMethod, … }`), `firstName`, `lastName`, `email`,
`phoneNumber`, `imageUrl`, `createdTime`, `dateOfBirth`, `country`,
`nationality`, `gender`, `address`, `fields`, `respondent`, `pendingAdmin`,
`needsProcessing`, `verifiedDateOfBirth`, `hasClubMembershipCard`, `guardians`,
`subGroups`, `roles`.

### Other group endpoints

| Call               | Request                                                     |
|--------------------|-------------------------------------------------------------|
| `getGroup`         | `GET core/v1/group/{groupId}`                                |
| `getGroupMembers`  | `GET core/v1/group/{groupId}/members?cursor=…&limit=…`       |
| `getFavorites`     | `GET core/v1/groups/favorites`                               |
| `getMissingSponds` | `GET core/v1/group/{groupId}/missingSponds?includeMainGroup=…&memberId(s)=…&subGroups=…` |

Note the singular `group/` for single-group paths and plural `groups` for the list.

## Users

Spond separates two concepts:

- **Profile** (`ApiProfile`): a Spond user account. Exists only for people who
  have signed up to the app.
- **Membership** (`ApiMembership`): a person's entry in one group. It exists
  even without an account. When the person has an account, `profile` links it.
  Guardians (parents) hang off a membership, not off the group.

### Group members (`GroupApi.getGroupMembers`)

```
GET core/v1/group/{groupId}/members?limit=100&cursor=<nextCursor>
```

| Param    | Type   | Notes |
|----------|--------|-------|
| `limit`  | int    | Page size. The app sends `2000`; if the server answers HTTP 400 it retries with `500` and keeps using 500 (`GroupRepositoryImpl.syncGroupMembers`, `membersPageSize`). |
| `cursor` | string | Omit for the first page; then pass the previous `nextCursor`. |

The app keeps requesting pages **while `nextCursor` is non-null**, even if a
page is empty, and upserts members by `id`.

Response (`ApiGroupMembersPage`):

```json
{
  "members": [ /* ApiMembership */ ],
  "nextCursor": "…"        // absent/null on the last page
}
```

`ApiMembership`:

| Field                   | Type                | Notes |
|-------------------------|---------------------|-------|
| `id`                    | string              | Membership id (per group) |
| `clubMembershipId`      | string              | |
| `profile`               | `ApiProfile`        | Null when the member has no app account |
| `firstName`, `lastName` | string              | |
| `email`, `phoneNumber`  | string              | May be hidden by group privacy settings |
| `imageUrl`              | string              | |
| `createdTime`           | instant             | |
| `dateOfBirth`           | string              | |
| `country`, `nationality`| string              | |
| `gender`                | enum (`Gender`)     | |
| `address`               | list of string      | Address lines |
| `fields`                | object              | Custom member fields, keyed by field definition |
| `respondent`            | bool                | |
| `pendingAdmin`, `needsProcessing`, `verifiedDateOfBirth` | bool | |
| `hasClubMembershipCard` | bool                | |
| `guardians`             | list of `ApiGuardian` | |
| `subGroups`             | list of string      | Subgroup ids; resolve via `group.subGroups[]` |
| `roles`                 | list of string      | Role ids; resolve via `group.roles[]` (`{ id, name, permissions }`) |

`ApiGuardian`: `id`, `email`, `phoneNumber`, `firstName`, `lastName`, `profile`.

`GET core/v1/groups?includeMembers=true` embeds the same `members` array in each
group, unpaged.

**Subgroup members:** the members endpoint has no subgroup parameter. The app
(`MembershipsViewModel` → `ObserveMembershipsUseCase`) filters locally: a member
belongs to a subgroup when its `subGroups` array contains the subgroup id.

### Profiles (`ProfileApi`)

| Call                    | Request                                    | Returns |
|-------------------------|--------------------------------------------|---------|
| `getMyProfile`          | `GET core/v1/profile`                      | `ApiMyProfile` |
| `getProfile`            | `GET core/v1/profile/{profileId}`          | `ApiProfile` |
| `getProfiles`           | `PUT core/v1/profiles/lookup`, body: JSON array of profile ids `["id1","id2"]` | JSON array of `ApiProfile` |
| `getProfileGroupCounts` | `GET core/v1/profile/hasGroups`            | |
| `getContactPoints`      | `GET core/v1/contactPoints`                | Your verified emails/phones |

`ApiProfile`: `id`, `firstName`, `lastName`, `imageUrl`, `contactMethod`,
`phoneNumber`, `email`, `unableToReach`.

`ApiMyProfile`: `id`, `firstName`, `lastName`, `imageUrl`, `contactMethod`,
`phoneNumber`, `primaryEmail`, `unableToReach`, `locale`, `countryCode`,
`timezone`, `trackingId`, `twoFactorPhoneNumber`, `internal`, `tosVersion`,
`dateOfBirth`, `gender`, `alternateEmails`, `preferences`.

## Admin: permissions and member management

### Who is an admin, and with which rights

`GET core/v1/groups` returns per group: `roles[]` (`{ id, name, permissions[] }`),
`memberPermissions[]`, `guardianPermissions[]`, `myMembership`,
`behalfOfMemberships[]`, and each membership's `roles[]` (role ids) and
`pendingAdmin`.

Permission wire names (`ApiGroupRole.Permission`): `members`, `admins`,
`settings`, `events`, `posts`, `polls`, `payments`, `chat`, `files`,
`fundraisers`.

How the app computes your rights (`GroupMapperKt.toDbRoles` /
`createDbGroupRole` and the Room view `DbGroupPermissions`):

- `roles[0]` is the **super-admin** role, the rest are custom admin roles. Only
  these admin roles grant management rights. The implicit member and guardian
  roles (built from `memberPermissions` / `guardianPermissions`) never do.
- Your rights = union of the permissions of the admin roles on memberships
  whose `profile.id` is your profile id (`isSelf`), **excluding** memberships
  with `pendingAdmin: true`.
- *Manage members* = `members` permission. *Manage admins* = `admins` permission.

Who may add people (`GroupMembersUiState`): the group must have
`adminsCanAddMembers: true`, plus *manage members* (to add a member) or
*manage admins* (to add an admin).

### Change a membership (`GroupApi.addOrUpdateMembership`)

```
POST core/v1/group/{groupId}/member
```

The body is `EditableMembership.EditResult.toJsonElement()`: the membership
`id` (when editing) plus **only the fields that changed**. Keys: `firstName`,
`lastName`, `phoneNumber`, `email`, `dateOfBirth`, `address`, `gender`
(`M`/`F`/`O`), `country`, `nationality`, `fields`, `subGroups`, `roles`,
`guardians`, `respondent` (= is a member, as opposed to admin only). A changed
field set to null is sent as JSON `null`. The response is the updated `ApiMembership`.

| Action (app use case) | Body |
|-----------------------|------|
| Add to / remove from subgroups (`AddMembershipsToSubgroupUseCase`, `RemoveMembershipFromSubgroupUseCase`) | `{"id": M, "subGroups": [full new list]}` |
| Set admin roles (`AddMembershipAsAdminUseCase`) | `{"id": M, "roles": [role ids]}` |
| Remove admin role, keep member (`RemoveMembershipAsAdminUseCase`) | `{"id": M, "roles": []}` (only if they are also a member) |
| Make an admin a member (`AddMembershipAsMemberUseCase`) | `{"id": M, "respondent": true}` |
| Remove member status, keep admin (`RemoveMembershipAsMemberUseCase`) | `{"id": M, "respondent": false}` (only if they are also an admin) |

Adding several members to a subgroup at once uses the bulk variant
`POST core/v1/group/{groupId}/members` with a JSON **array** of the same objects
(`addOrUpdateMemberships`).

### Remove from the group (`GroupApi.deleteMembership`)

```
DELETE core/v1/group/{groupId}/members/{membershipId}
```

Removes the membership entirely (member and admin). Rules in
`GroupMemberDeleteFlow` / `initialGroupMemberDeleteStep`:

- The group's **contact person** (`group.contactPerson.id` equals the member's
  `profile.id`) cannot be removed. For a club, also not when they are the
  contact person of one of the club's groups (`group.club.id` = club id).
- A member who is also an admin is first offered "remove as member only"
  (`respondent: false`) or "remove as admin only" (`roles: []`) when you can
  manage admins.

### Add a guardian (`GroupApi.addGuardian`)

```
POST core/v1/group/{groupId}/member/{membershipId}/guardian
{ "firstName": "...", "lastName": "...", "phoneNumber": "+47...", "email": "..." }
```

`EditedGuardian`; null fields are omitted (the app's JSON uses `explicitNulls = false`).

### Join requests (`GroupApi.approveMembershipRequest` / `rejectMembershipRequest`)

Pending requests come with the group: `GET core/v1/groups` → `membershipRequests[]`
(`ApiMembershipRequest`): `id`, `groupMembershipId`, `profile` (`{ id,
firstName, lastName, phoneNumber, email, imageUrl, creator }`), `guardians[]`
(same shape), `dateOfBirth`, `country`, `nationality`, `gender`, `address[]`,
`fieldValues` (custom field id → value). A request with `groupMembershipId`
claims an existing membership ("existing" requests); without it, it is a new
member.

Handling requests needs the *manage members* permission
(`GroupMembersDataState.canManageRequests`).

```
POST core/v1/group/{groupId}/membershipRequest/{requestId}/approve
{ "subGroups": ["SG1", "SG2"] }          → ApiMembership (the new member)

POST core/v1/group/{groupId}/membershipRequest/{requestId}/reject
(no body)
```

The subgroups are chosen when approving (`ApproveMemberRequestToSubgroupsActivity`);
an empty list is allowed.

### Edit a member's details (`EditMemberViewModel` / `EditAdminViewModel`)

Same endpoint, `POST core/v1/group/{groupId}/member`, built by
`toEditableMembership(form, original, MEMBER | EXISTING_ADMIN, …)`:

- `id`, plus `firstName` / `lastName` when changed;
- `phoneNumber` and `email` are **always** sent (force-marked as changed),
  `null` when empty;
- `fields` (the complete custom-field map) when any custom field changed.

Custom field values (`CustomMemberFields`): a JSON object of field id → value.
`NUMBER` fields are JSON numbers (`LongMemberField`; a non-number clears it),
everything else is a string. Empty number and choice fields are left out (null
values are dropped); an empty `TEXT` field is sent as `""`. `DATE` is
`YYYY-MM-DD`.

Field definitions (`group.fieldDefs[]`): `id`, `name`, `type` (`TEXT`,
`NUMBER`, `DATE`, `SINGLE_OPTION`, `MULTIPLE_OPTION`), `memberFieldOptions[]`,
`visibility`, `permission` (`ADMINS_AND_ALL_MEMBERS`, `ADMINS_AND_ADULT_MEMBERS`
— the default, `CLUB_AND_GROUP_ADMIN`, `CLUB_ADMIN_ONLY`), `source`, `locked`,
`required`. In the admin edit form a field is shown when it is `required`, or
when it is not `locked` and its permission allows admins
(`MemberFieldPermission.isEditable`: everything but `CLUB_ADMIN_ONLY`).
*Not verified:* how a `MULTIPLE_OPTION` value combines several options.

### Invite / add a person (`CreateMemberViewModel`, `CreateAdminViewModel`)

Adding a person **is** the invitation: `POST core/v1/group/{groupId}/member`
without an `id`. The body comes from `UiMembershipFormKt.toEditableMembership`
for a new entity:

- always `firstName`, `lastName`, `phoneNumber`, `email` (the last two are
  force-marked as changed, so they are `null` when empty), and `fields: {}`;
- **member** (`MembershipSaveTarget.MEMBER`): `respondent: true`, no `roles`;
- **admin** (`NEW_ADMIN`): `roles: [...]`, no `respondent` (it stays `false`);
- `subGroups`, `guardians` (`[{firstName, lastName, phoneNumber?, email?}]`)
  and `dateOfBirth` (`YYYY-MM-DD`) only when given.

```json
{"firstName":"Kari","lastName":"Nordmann","email":"kari@example.com","phoneNumber":null,
 "fields":{},"respondent":true,"subGroups":["SG1"],
 "guardians":[{"firstName":"Ola","lastName":"Nordmann","phoneNumber":"+4712345678"}]}
```

The app normalises phone numbers with the user's country before sending; a
terminal client should send international format (`+4712345678`).

Not the same thing: `POST core/v1/group/{groupId}/inviteToApp`
(`{"text": message, "recipients": [profile ids]}`) sends a "get the app"
message to existing members (the payments "tip members" screen).

## Subgroups

A group's subgroups are in `group.subGroups[]` (`{ id, name, color, imageUrl }`)
from `GET core/v1/groups`. To scope a feed to a subgroup, the app sends
**both** `groupId` and `subGroupId`:

| Feed         | How the app scopes to a subgroup |
|--------------|----------------------------------|
| Posts        | `GET core/v1/posts?…&groupId=G&subGroupId=SG` (`PostPagingViewModel` → `GetPlainPostPagingFlowUseCase(groupId, subgroupId)`) |
| Arrangements | `GET core/v1/sponds?…&groupId=G&subGroupId=SG` (`SpondPagingViewModel` → `GetSpondPagingFlowUseCase(filter, groupId, subgroupId)`); `includeHidden=true` is still sent because `groupId` is set |
| Members      | Filtered locally by `membership.subGroups` (see above) |

Note the casing: the query parameter is `subGroupId`, the JSON arrays are
`subGroups` / `subGroupIds`.

**The group feed does not include subgroup posts** (verified live, 2026-10).
`GET core/v1/posts?type=PLAIN&groupId=G` (no `subGroupId`) returns only posts
sent to the whole group. A post sent to one or more subgroups (non-empty
`subGroupIds`) shows up only in the feed of each of those subgroups. In the
test group, none of the 20 subgroup posts appeared in the group feed. So to
get **every** post in a group, fetch the group feed plus the feed of every
subgroup in `group.subGroups[]`, and de-duplicate by `id`: a post sent to
several subgroups comes back once per subgroup. (Example: the group feed held
218 posts, the subgroup feeds added 20 more, 238 in total.)

Not verified: how the server answers a member who is not in a subgroup and
asks for that subgroup's feed (empty list or an error). The test account was
an admin.

## Arrangements / events (`SpondApi`)

In the code an arrangement is a **spond** (`ApiSpond`).

### List arrangements

```
GET core/v1/sponds
```

All parameters `SpondApi.getSponds` can send:

| Param               | Type     | Sent when |
|---------------------|----------|-----------|
| `max`               | int      | Always. Page size; `GetSpondPagingFlowUseCase` defaults to 10 per page, 20 for the first load. |
| `order`             | `asc`/`desc` | Always. `desc` for past, `asc` otherwise. |
| `includeComments`   | bool     | Always `true`. |
| `groupId`           | string   | Group filter. |
| `subGroupId`        | string   | Subgroup filter. |
| `prevId`            | string   | Paging cursor: id of the last arrangement of the previous page. |
| `minStartTimestamp` | ISO-8601 | Upcoming paging cursor (start time of the last arrangement). |
| `maxStartTimestamp` | ISO-8601 | Past paging cursor (start time of the last arrangement). |
| `minEndTimestamp`   | ISO-8601 | Upcoming: start of today (local midnight). |
| `maxEndTimestamp`   | ISO-8601 | Past: start of today (local midnight). |
| `includeHidden`     | `true`   | Whenever `groupId` is set. |
| `excludeRepeating`  | `true`   | Filter option. |
| `excludeAvailability` | `true` | Filter option. |
| `excludeActive`     | `true`   | Past list only (leaves out arrangements still running). |
| `hasRegistrationFee`| `true`   | Filter option. |
| `response`          | repeated | Past list **without** a group only: `accepted`, `unanswered` and, unless declined are excluded, `declined`. |

Flags with value `false` are not sent at all.

How the app builds the query (`SpondRepositoryImpl.PagingSyncAdapter.fetchAndSaveSponds`,
default filter = upcoming, no exclusions):

```
Upcoming, group G, first page:
  max=10&order=asc&includeComments=true&groupId=G&minEndTimestamp=<today 00:00 local, as UTC>&includeHidden=true
  next page: + minStartTimestamp=<last.startTimestamp>&prevId=<last.id>

Past, group G, first page:
  max=10&order=desc&includeComments=true&groupId=G&maxEndTimestamp=<today 00:00>&includeHidden=true&excludeActive=true
  next page: + maxStartTimestamp=<last.startTimestamp>&prevId=<last.id>
```

The cursor is the last item's `startTimestamp` (kept as epoch milliseconds) plus
its `id`. A page shorter than `max` ends the list.

Response: JSON array of `ApiSpond`.

### `ApiSpond` fields

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | |
| `type` | enum | `EVENT`, `RECURRING`, `AVAILABILITY` |
| `heading`, `description` | string | Title and body |
| `startTimestamp`, `endTimestamp` | instant | |
| `openEnded` | bool | No end time |
| `meetupPrior` | int | Meet-up time, minutes before start |
| `rsvpDate` | instant | Answer deadline |
| `location` | object | `id`, `feature`, `address`, `postalCode`, `country`, `administrativeAreaLevel1`, `administrativeAreaLevel2`, `locality`, `longitude`, `latitude` |
| `cancelled`, `cancelledReason` | bool, string | |
| `hidden`, `deleted`, `expired`, `muted` | bool | |
| `maxAccepted` | int | Participant cap (0 = none) |
| `autoAccept`, `participantsHidden`, `commentsDisabled`, `registered` | bool | |
| `visibility` | enum | `INVITEES`, `ALL`, `PUBLIC`; parsed case-insensitively, unknown values mean `INVITEES` (`EventVisibilitySerializer`) |
| `creatorId` | string | |
| `owners` | list | `{ id, response }` — hosts and their answer |
| `recipients` | object | `group` (`{ id, name, type, activity, subGroups[], members[] }`), `profiles[]`, `guardians[]`, `groupMembers[]`. **`group.type` is a number** (`GroupTypeSerializer`): `1` adults, `2` children, `3` mixed |
| `responses` | object | `acceptedIds`, `declinedIds`, `unansweredIds`, `waitinglistIds`, `participantIds`, `unconfirmedIds`, `declineMessages` (map id → `{ profileId, message }`), `registeredAttendance` |
| `responseCounts` | object | `acceptedCount`, `declinedCount`, `unansweredCount`, `waitinglistCount`, `unconfirmedCount` |
| `seriesId`, `seriesOrdinal`, `modifiedFromSeries` | | Recurring series |
| `inviteTime`, `createdTime`, `updated` | instant / long | |
| `autoReminderType`, `autoReminderTime` | | |
| `payment` | object | `{ total, currency }` |
| `tasks` | object | `openTasks[]`, `assignedTasks[]` |
| `matchInfo` | object | `type`, `teamName`, `opponentName`, `teamColour`, `opponentColour`, `scoresSet`, `scoresPublic`, `scoresFinal`, `scoresSetEver`, `teamScore`, `opponentScore` |
| `groupName` | string | Signup group name |
| `clubId`, `clubName`, `replyToEmail`, `picture`, `attachments`, `comments`, `children`, `publicMetadata`, `publicEventShareUrl`, `availabilityRequestId`, `behalfOfIds`, `myPublicSignupResponse`, `myPublicSignupOnBehalfOf`, `clientOutOfDate` | | |

The ids in `responses.*Ids` are member ids. The app looks them up in
`recipients.group.members[]` (`{ id, firstName, lastName, profile: { id } }`).
`recipients.group.subGroups[]` are the subgroups the arrangement was sent to.

### Your answer: `behalfOfIds`

`behalfOfIds` lists the recipient ids the logged-in user may answer for:
their own membership, and the members they are guardian of (children).
Each id is classified as a guardian, ad-hoc profile or membership recipient
(`SpondMapperKt.resolveRecipientType`).

The app derives the answer for a recipient (`SpondMapperKt.calcResponseType`)
by checking the `responses` lists in this order; the first list that contains
the id wins:

| Order | List              | Meaning                           |
|-------|-------------------|-----------------------------------|
| 1     | `unconfirmedIds`  | Accepted, awaiting confirmation   |
| 2     | `unansweredIds`   | Invited, not answered             |
| 3     | `acceptedIds`     | Attending                         |
| 4     | `waitinglistIds`  | On the waiting list               |
| 5     | `declinedIds`     | Not attending                     |
| —     | none              | Not a recipient                   |

### Answer an arrangement (`SpondApi.respondToSpond`)

```
PUT core/v1/sponds/{spondId}/responses/{recipientId}
Content-Type: application/json

{ "accepted": true }
{ "accepted": false }
{ "accepted": false, "declineMessage": "Sick" }
```

- `recipientId` is one of the arrangement's `behalfOfIds`.
- **Scheduled invitations:** when `inviteTime` is in the future the
  invitations have not gone out yet and nobody can answer; the server replies
  `HTTP 400 {"errorCode":3004,"errorKey":"inviteNotSent"}`. The app checks
  `Spond.isInvited()` = `inviteTime == null || inviteTime <= now` and shows a
  "scheduled" banner instead of the answer buttons (`SpondScheduledBannerContent`).
- `declineMessage` is sent only when declining with a non-empty message
  (`WriteDeclineMessageActivity`).
- The response is the updated `responses` object (`ApiSpond.ResponseBundle`).
  The app stores it as is (`saveResponseBundle`). `responseCounts` is not
  included, so re-read the arrangement for fresh counts.
- **Paid arrangements:** when `payment.total > 0`, the app does **not** use this
  call to accept. It runs `JoinPaymentSpondUseCase` (payment) instead
  (`SpondAnswerOnBehalfAction.joinSpond`). Declining still uses this call.
- `payment.total` is in minor units of `payment.currency` (the app's price input
  uses the currency's `decimals`).
- `meetupPrior` is in minutes (the app multiplies it by 60000 to get milliseconds).

### Other arrangement endpoints

| Call                | Request |
|---------------------|---------|
| `getSpond`          | `GET core/v1/sponds/{id}` (`includeComments=true` optional; header `X-Spond-LastUpdateTime: <epoch ms>` optional) |
| `getBulkSponds`     | `GET core/v1/sponds/getBulk?ids=a,b,c&includeComments=true` |
| `getUpcomingSponds` | `GET core/v1/sponds/upcoming?minEndTimestamp=…` (+ `includeDeclined=true` optional), returns `ApiUpcomingSpond` |

## Posts (`PostApi`)

### List posts

```
GET core/v1/posts
GET core/v1/posts/unanswered      (same params, "unanswered" filter)
```

| Param             | Type    | Notes |
|-------------------|---------|-------|
| `type`            | enum    | **Required.** `PLAIN`, `POLL` or `PAYMENT` (`PostTypeFilter`). |
| `groupId`         | string  | Restrict to one group. |
| `subGroupId`      | string  | Restrict to one subgroup. Without it, only whole-group posts come back (see [Subgroups](#subgroups)). |
| `memberId`        | string  | Restrict to one member. |
| `max`             | int     | Page size. App feed: 30 initial, 10 per further page. The server accepts at least `500` and `1000` (verified live). |
| `includeComments` | bool    | Feed sends `false`. With `true`, each post carries its comments (see below). |
| `prevId`          | string  | Paging cursor: id of the oldest post already seen. |
| `maxTimestamp`    | ISO-8601| Paging cursor: timestamp of the oldest post already seen. |
| `minTimestamp`    | ISO-8601| Lower bound, **inclusive** (verified live). |
| `requestStatus`   | enum    | `ACTIVE` / `COMPLETED` (payment/poll filters only). |
| `sortBy`          | enum    | `SEND_DATE` / `DUE_DATE` (payment/poll filters only). |
| `order`           | enum    | `asc` / `desc` (payment/poll filters only). |

Response: JSON array of `ApiPost`:

| Field              | Type                                  |
|--------------------|---------------------------------------|
| `id`               | string                                |
| `type`             | `PLAIN` / `POLL` / `PAYMENT` …        |
| `timestamp`        | instant                               |
| `dueTimestamp`     | instant                               |
| `ownerId`          | string                                |
| `groupId`          | string                                |
| `spondId`          | string                                |
| `title`, `body`    | string                                |
| `poll`, `payment`, `clubPayment` | objects                 |
| `media`            | list                                  |
| `muted`, `unread`, `clientOutOfDate`, `commentsDisabled`, `selectMemberPoll` | bool |
| `visibility`, `state` | enum                               |
| `subGroupIds`      | list of string                        |
| `reactions`        | map emoji → map memberId → epoch ms   |
| `attachments`      | list                                  |
| `comments`         | list (when `includeComments=true`)    |

Badge counts come back as **response headers**, not in the body:
`X-Spond-PostBadgeCount`, `X-Spond-PollBadgeCount`, `X-Spond-PaymentBadgeCount`,
`X-Spond-SubscriptionBadgeCount`, `X-Spond-InstalmentsDueBadgeCount`.

### Paging

`PostRepositoryImpl.PagingSyncAdapter` (Room `RemoteMediator`) stores, per filter,
a `cursorTimestamp` and `prevId`. To fetch the next (older) page, repeat the
same query with:

```
maxTimestamp=<oldest post timestamp>&prevId=<oldest post id>
```

An empty or short page means the end.

Downloading a whole history is cheap: with `max=500` and
`includeComments=true`, all 218 posts of the test group came back in one
request of 466 KB in about 1.5 s.

### Single post

```
GET core/v1/posts/{postId}?includeComments=true
```

### Comments

With `includeComments=true`, `comments` is a list of (verified live):

| Field           | Type    | Notes |
|-----------------|---------|-------|
| `id`            | string  | |
| `fromProfileId` | string  | Author's profile id. |
| `timestamp`     | instant | When it was written. Not changed by an edit. |
| `text`          | string  | |
| `reactions`     | map     | Same shape as on posts. |
| `children`      | list    | Replies, same fields. Only one level deep. |
| `edited`        | bool    | `true` after an edit; the field is **omitted** when false. |

The list comes back **newest first**; replies in `children` likewise. The app
shows them oldest first.

### Caching and change detection

There is no way to ask the server whether a post or its comments changed
(checked live against `GET core/v1/posts` and `GET core/v1/posts/{id}`,
2026-10):

- No `ETag` or `Last-Modified` response header, so `If-None-Match` /
  `If-Modified-Since` cannot be used. Responses carry
  `cache-control: no-cache` and `cf-cache-status: DYNAMIC` (Cloudflare, not cached).
- Posts have no update-time field. `timestamp` is the send time and does not
  change when the post is edited or gets new comments.
- Comments have `edited`, but no edit time, and nothing on the post says that
  a comment was added, edited or deleted.
- `minTimestamp` only finds newly *sent* posts; it misses edits, new comments
  and deletions on older posts.

(Arrangements are different: `getSpond` accepts an `X-Spond-LastUpdateTime`
header, see [Other arrangement endpoints](#other-arrangement-endpoints). There
is no such header for posts in the app.)

A local cache therefore has to re-download the whole feed (group feed plus
every subgroup feed) and compare. Since that is only a few requests per
group, it is cheap. `spondcli` does this: it hashes each post's JSON to skip
unchanged posts and deletes posts that are no longer returned.

## Home timeline

The app's home screen lists arrangements from **every** group. It is the
normal arrangements list (see [List arrangements](#list-arrangements)) with
no `groupId`, `subGroupId` or `includeHidden` (verified live, 2026-10):

```
GET core/v1/sponds?max=20&order=asc&includeComments=true&minEndTimestamp=<today 00:00 local, as UTC>
```

Paging and the Past variant work as for one group. Each arrangement's
`recipients.group` (`id`, `name`, `subGroups`) says which group it belongs to.

## Notifications (`ActivityApi`)

The notifications screen (`ActivitiesViewModel` →
`ObserveActivitySummariesUseCase`) uses the **summary** list:

```
GET core/v1/activities/summary            (optional lang=<language code>)
```

Response: JSON array, newest first, of `ApiActivitySummary` (verified live):

| Field          | Type    | Notes |
|----------------|---------|-------|
| `id`           | string  | |
| `type`         | string  | Seen: `spond_comment`, `post_comment`, `comment_mention`, `accept_spond`, `decline_spond`, `update_spond`, `nag`. |
| `profileId`    | string  | Your profile. |
| `groupId`      | string  | |
| `spondId`      | string  | Set for arrangement events. |
| `postId`       | string  | Set for post events. |
| `membershipId` | string  | Sometimes set. |
| `pCmntId`      | string  | Parent comment id (mentions). |
| `text`         | string  | Ready-made sentence, HTML with `<b>` around names and titles, e.g. `<b>Kari</b> and 2 others commented on <b>Training</b>`. Already grouped ("and 2 others"). |
| `timestamp`    | instant | |
| `read`         | bool    | |

Without `lang` the text is in the account's language (Norwegian for the
test account). `lang=en` gives English. `lang=nb` also gave English, so the
code the app sends for Norwegian is not known. Response headers carry `X-Spond-NotificationBadgeCount` and
`X-Spond-NotificationAppBadgeCount`.

Other `ActivityApi` calls (from the decompiled code; only `getActivities` was tried live):

| Call                         | Request |
|------------------------------|---------|
| `getActivities`              | `GET core/v1/activities?order=asc\|desc&max=N&since=<cursor>`: raw events (`id, type, user{id,name}, timestamp, spond, post, chat{id,chatNum}, group, comment, …`), not grouped |
| `markActivitySummaryAsRead`  | `PUT core/v1/activities/{id}/read` |
| `markAsSeen`                 | `PUT core/v1/activities/checked` (clears the badge) |

### How the app stays up to date

There is no websocket or other live connection to Spond's servers. (The APK
bundles Ably and Ktor websockets, but only third-party SDKs such as Intercom
use them; nothing under `com.spond` does.) The app combines three things
(from the decompiled `SyncManager` / `SyncFlow` / `PushMessagesHandlerImpl`):

1. **Polling the activity stream.** `SpondroidSdk.start` sets
   `PollingConfig(interval = 60000 ms, delay = 0, taskIds = {"SyncNewActivities"})`.
   `SyncFlow` starts this poll when the app comes to the foreground and stops
   it in the background. Each tick is one call:

   ```
   GET core/v1/activities?order=asc&since=<cursor timestamp, ISO instant>&max=100
   ```

   The cursor (`ActivitySyncCursor`: newest activity id + timestamp) is stored
   locally and moved forward after each call. Verified live (2026-10): the
   server rejects `since` without `order=asc` (HTTP 400, "Cannot use 'since'
   parameter with descending order"), and the bound is **inclusive**: the
   cursor's own activity comes back, so skip it by id. The raw types are
   lower case (`new_post`, `update_post`, `post_comment`, `update_spond`,
   `decline_spond`, `chat_reaction`, …). Plain chat messages are **not** in
   the stream; only `chat_reaction` and `chat_mention` are. The
   `X-Spond-NotificationBadgeCount` header updates the badge.
2. **Acting on activities.** Each new activity is turned into a pending
   action (`PendingActionMapperKt.toPendingActions`), such as `REFRESH_SPOND`,
   `UPDATE_SPOND`, `DELETE_SPOND`, `NEW_POST`, `REFRESH_POST`, `DELETE_POST`,
   `POST_COMMENT`, `SPOND_COMMENT`, `REFRESH_GROUP`, `INVALIDATE_CHATS`,
   `CLEAR_CACHE`. `HandleActivitiesTask` runs them: it marks the affected
   arrangement, post, group or profile as dirty and triggers the matching
   `SyncDirty*` task, which re-downloads only that object. So the app never
   re-downloads whole lists on a timer; the activity stream says what changed.
3. **Push (FCM).** `SpondFirebaseMessagingService` hands push messages to
   `PushMessagesHandlerImpl`. In the foreground every push also triggers
   `SyncNewActivities` (the same poll as above, run at once). Chat pushes
   (`chat_message`, `chat_read`, `chat_msg_deleted`, …) mark the chat thread
   or its messages as out of date. The silent push `poke_device` makes the
   app re-register its push token.

Besides these, `SyncFlow` runs its tasks once at start and again each time the
app comes to the foreground (`AutoSyncCondition.STARTED` / `FOREGROUND`):
upcoming arrangements, groups, chats, activity summaries, badge counts and so
on. A failed task is retried with backoff from 3 s up to 30 min. Pull to
refresh triggers the same tasks by hand. The 30-second timer in the
arrangement list (`SpondPagingFragment.setupTimedRefresh`) only redraws the
visible rows (relative times); it makes no request.

A client without push can copy step 1 and 2: poll
`core/v1/activities?order=asc&since=…` every minute and re-fetch only the
arrangements and posts the new activities name (`spond.id`, `post.id`,
`group.id` on each activity). Since new chat messages are not in the stream,
it must also poll the first page of chat threads (`GET chats`). `spondcli`
does all of this; see below.

## Messages (`ChatApi`)

Chat runs on a **separate server** with its own token.

1. Log in to chat with the normal bearer token:

   ```
   POST core/v1/chat/          body: {}
   → { "url": "https://api.spond.com/chat/v1", "auth": "<chat token>" }
   ```

   (`AuthConfig.Companion.refreshToken`, tag `RefreshChatToken`.)
2. Send every chat request to `<url>/<path>` with the header
   **`auth: <chat token>`**, not `Authorization` (`ChatAuthProvider`). A
   wrong or expired token gives HTTP 401; get a new one with step 1.

| Call (`ChatApi`)      | Request |
|-----------------------|---------|
| `getThreads`          | `GET chats?max=N` + optional `maxTimestamp`, `minTimestamp`, `unreadOnly`, `groupChatsOnly`, `guardianChats`, `activeChatsOnly` (all `true` when sent) |
| `getThread`           | `GET chats/{id}` |
| `getMessages`         | `GET chats/{id}/messages?count=N` + optional `maxNum`, `minNum` |
| `getThreadReadStates` | `GET chats/{id}/read` |
| `sendMessage`         | `POST messages`, see below |
| `findThread`          | `POST chats/community/{groupId}/type/GROUP/find`, body: JSON array of the other people's profile ids (verified live: returns the existing thread, or `{}` when there is none) |
| `reportMyLastRead`    | `PUT chats/{id}/{msgNum}/read` |
| `markThreadsAsRead`   | `PUT chats/bulkRead` (JSON array of ids) |
| `markAsSeen`          | `PUT chats/seen` |
| `setReaction`         | `PUT chats/{id}/{msgNum}/reaction`, body `{"value": "<emoji>"}` → `{"value": <epoch ms>}` (from the decompiled code) |
| `removeReaction`      | `DELETE chats/{id}/{msgNum}/reaction` |
| others                | See [Messages: extra chat calls](#messages-extra-chat-calls-chatapi) |

**Threads** (`GET chats`, newest first; verified live):

| Field             | Notes |
|-------------------|-------|
| `id`              | |
| `type`            | `GROUP`, `DM`, `CLUB_MESSAGE`, `CAMPAIGN` |
| `name`            | Often missing. A `GROUP` without a name is named after its group; a `DM` is named after the other person. |
| `groupId`, `community{type,id}` | Also set on a `DM`: the group it was started from. |
| `participants`    | **Profile** ids (the same ids as `members[].profile.id` and `guardians[].profile.id` from `GET groups`). |
| `newestTimestamp` | Paging cursor. |
| `unread`, `muted` | bool |
| `message`         | The newest message. |

Paging: repeat with `maxTimestamp=<newestTimestamp of the last thread>`;
the bound is **exclusive**. The `X-Spond-ChatBadgeCount` header holds the
unread count.

**Messages** (`GET chats/{id}/messages`, newest first): `chatId`, `msgNum`
(counts up from 0 in each thread), `user` (sender profile id; missing on
club messages and campaigns), `text`, `type` (`TEXT`, `IMAGES`,
`CLUB_MESSAGE`, `CAMPAIGN`, …), `timestamp`, `reactions` (emoji → profile id
→ epoch ms), and per type `mentions`, `images[{url,width,height}]`,
`clubMessage{clubName,senderName,senderTitle,subject}`,
`campaign{id,campaignName,…}`. For older messages pass
`maxNum=<oldest msgNum − 1>`; `maxNum` is **inclusive**.

**Sending** (`ChatApi.sendMessage` / `toJsonObject`; built from the
decompiled code, not sent live):

```
POST <url>/messages
{"chatId": "<thread id>", "text": "Hello", "type": "TEXT"}          reply in a thread

{"community": {"type": "GROUP", "id": "<group id>"},
 "recipient": "<profile id>", "text": "Hello", "type": "TEXT"}       first message: creates the thread
```

With several people the app sends `"recipients": [ids]` instead of
`"recipient"`. Your own profile id is never among them. Before showing an
empty conversation the app calls `findThread` with the same people and uses
the existing thread when there is one. The reply is the stored message;
its `chatId` names the new thread. Optional fields: `mentions[{id,type}]`,
and per type `spond{id}`, `post{id}`, `campaign{id}`, `images`, `file`.
Chat messages have no reply-to or quote field; "replying" means writing in
the thread.

**Reactions.** A message's `reactions` maps an emoji to who reacted with
it: `{"👍": {"<profile id>": <epoch ms>}}` (verified live). Everyone has at
most one reaction per message: `setReaction` replaces yours
(`Reactions.change`), and in the app tapping your current emoji again
removes it with `removeReaction`. The app's picker (`PickEmojiDialog`)
offers `Reactions.DEFAULT_EMOJIS` (🤩 😃 🤣 🙁 👍 👎), the group's activity
emoji and a full emoji keyboard; received reactions can be any emoji.

**Mentions** (`MentionTextProcessor`, `MentionEditText`, `MentionManager`;
the received form verified live). Each mention in `text` is the placeholder
character **U+E001**; `mentions` lists the people in the same order:

```
{"chatId": "<thread id>", "type": "TEXT",
 "text": " can you fix the light?",
 "mentions": [{"id": "<profile id>", "type": "PROFILE"}]}
```

The app sends only `id` and `type`; received messages also carry `name`
(`{"id", "name", "type": "PROFILE"}`). To show a message, replace the n-th
U+E001 with `@` + the n-th name. `type` is `PROFILE` (a profile id) or
`MEMBERSHIP` (a member id, used for post and arrangement comments). In chat
the app offers the thread's current participants, and only in `GROUP`
threads (none in a `DM`). Before sending it checks that everyone mentioned
is you or still a participant.

How the app's input field behaves: typing `@` at the start or after a space
or line break starts a query; the text after `@` up to the cursor filters
the candidates by display name (guardians also match on their children's
names; the exact match helper is obfuscated, most likely "contains, ignoring
case"). A space or line break in the query, or moving
the cursor before the `@`, ends it. Picking a person replaces `@query` with
`@Full Name` (one unit) plus a space. Backspace right after a mention turns
it back into `@` and reopens the query.

## Endpoint catalogue

This section lists every HTTP call in `com.spond.sdk.data.network.api.*` and
`com.spond.sdk.data.network.auth.*` that the sections above do not cover. It
was extracted from the 4.64.1 bytecode: the URL fragments, query parameter
names, headers and JSON keys each method loads, plus the method's parameter
types. None of these calls were tried live.

Conventions used below:

- Paths are relative to `https://api.spond.com/`. `{x}` is a path parameter.
  Where a method takes several ids, their order in the path follows the
  method's parameter order; that mapping is *inferred*.
- "Body" lists the JSON keys the method writes. `Edited*`, `New*` and `Api*`
  names are the app's request or response classes; their fields are listed
  when they are known.
- Boolean query flags are sent only when `true`, as everywhere else in the app.
- Every call has an app tag (shown in parentheses, e.g. *CreateSpond*). It is
  only used for logging and has no effect on the wire.

### Servers and tokens

`com.spond.sdk.domain.model.Server` has one host and four path prefixes:

| Prefix    | Base URL                          | Auth header                     | Token comes from |
|-----------|-----------------------------------|---------------------------------|------------------|
| `core`    | `https://api.spond.com/core/v1/`  | `Authorization: Bearer <token>` | Login (see [Authentication](#authentication)) |
| `chat`    | the `url` returned by `POST core/v1/chat/` | `auth: <token>`        | See [Messages](#messages-chatapi) |
| `frc`     | `https://api.spond.com/frc/v1/`   | `auth: <token>`                 | `GET core/v1/profile/fundraisingToken` |
| `storage` | `https://api.spond.com/storage/`  | `auth: <token>`                 | A files token, see below |
| `images`  | `https://api.spond.com/images/`   | —                               | No API calls use it directly |

`frc` is the fundraising service ("FRC", `CampaignApi`). `storage` holds file
uploads. Both use short-lived tokens (`TempAuthProvider`). The token endpoints
return `ApiTempToken` `{ "value": "<token>" }`:

| Token                    | Request |
|--------------------------|---------|
| Fundraising (*RefreshFrcToken*) | `GET core/v1/profile/fundraisingToken` |
| Personal files (*RefreshPersonalFilesToken*) | `GET core/v1/profile/filesToken` |
| Group files (*RefreshGroupFilesToken*) | `GET core/v1/group/{groupId}/filesToken` |
| Coaches' corner files (*RefreshCoachesCornerFilesToken*) | `GET core/coaches-corner/v1/corners/{cornerId}/filesToken` |

Other servers: `https://api-staging1.spond.dev/` and `https://api-dev1.spond.dev/`
(`com.spond.host.config.Servers`, internal builds). Feature flags come from
`https://features.spond.com/proxy`.

**Errors.** A failed call returns `ApiError`:
`{ "errorCode": 3004, "errorKey": "inviteNotSent", "message": "…", "info": …, "usages": … }`.

### Core utilities

| Call | Request | Returns |
|------|---------|---------|
| `ClockApi.syncServerTime` (*QueryServerTime*) | `GET core/v1/clock` | `ApiClock` `{ time }`. Used to correct the device clock. |
| `ConfigApi.getRemoteConfig` (*GetGeneralConfig*) | `GET core/v1/config` | `ApiRemoteConfig` `{ maxGroupSize, maxVideoUploadSize, maxVideoUploadLength }` |
| `ConfigApi.getStorageConfig` (*GetStorageConfig*) | `GET storage/upload/restrictions` | `ApiStorageConfig` `{ maxSize }` |
| `CurrencyApi.getCurrency` (*GetCurrency*) | `GET core/v1/currencies/{currencyCode}` | `ApiCurrency` `{ code, name, simpleFormat, fullFormat, exactFormat, divisor, decimals, exactDecimals }` |
| `DeviceApi.registerDevice` (*RegisterDevice*) | `POST core/v1/device`, body `{ "pushToken", "type", "name": "Android <model> (<version>)", … }` (`production` marks the push environment) | |
| `DeviceApi.unregisterDevice` (*UnregisterDevice*) | `DELETE core/v1/device/{deviceId}` (which id is *unverified*) | |
| `PromotionApi.getPromotions` (*GetPromotions*) | `GET core/v1/promos` | list of `ApiPromotion` `{ id, version, image, buttons[{text, effect, primary}], type, endTime, title, description, category, video }` |
| `PromotionApi.dismissPromotion` (*DismissPromotion*) | `PUT core/v1/promos/dismiss/{promotionId}` (with a JSON body) | |
| `UsageApi.intendToCreateGroup` (*IntendToCreateGroup*) | `POST core/v1/usage`, body `{ "type": "is_group_admin", "action": "tapped" }` | Analytics only |
| `UsageApi.promoteAppInGroup` (*PromoteAppInGroup*) | `POST core/v1/usage`, body `{ "type": "promote_app_in_group", "groupId", "action" }` | Analytics only |
| `UsageApi.tipFundraising` (*TipFundraising*) | `POST core/v1/usage`, body `{ "type": "tip_fundraising", "groupId" }` | Analytics only |

### Locations, time zones and weather

| Call | Request | Returns |
|------|---------|---------|
| `LocationApi.autocomplete` (*AutocompleteLocation*) | `GET core/v1/locations/autocomplete?keyword=…&sessionToken=…&lat=…&lng=…` (`lat`/`lng` optional, to bias results) | list of `Place` `{ placeId, featureName, addressLine }` |
| `LocationApi.lookup` (*LookupLocation*) | `GET core/v1/location/{placeId}?sessionToken=…` | `PlaceLocation` `{ placeId, featureName, addressLine, latitude, longitude, country, locality, postalCode, administrativeAreaLevel1, administrativeAreaLevel2 }` |
| `LocationApi.searchTimeZone` (*SearchTimeZone*) | `GET core/v1/timeZone?search=…&lang=…` (the app maps `nb` and `nn` to `no`) | list of `SearchedTimeZone` `{ id, name }` |
| `SpondApi.fetchWeather` (*FetchWeather*) | `GET core/v1/weatherpredictions?latitude=…&longitude=…&startTimestamp=…&endTimestamp=…` | `ApiWeather` `{ airTemperatureCelsius, airTemperatureFahrenheit, windSpeedMetersPerSecond, windSpeedMilesPerHour, windSpeedKilometersPerHour, isLight, weatherSymbolCode, uvIndex, adTargetingWeatherType }` |

`sessionToken` groups an autocomplete session with its final lookup (Google
Places billing style); the app generates it.

### Account (`AuthApi`)

Login, 2FA verification, `2fa/resend` and `logout` are described under
[Authentication](#authentication). The rest:

| Call | Request | Notes |
|------|---------|-------|
| `refreshToken` (*RefreshCoreToken*, `AuthConfig`) | `POST core/v1/auth2/login/refresh`, body `ApiRefreshLoginRequest` `{ "value": "<refresh token>" }` | Sends the access token instead when there is no refresh token. Returns `ApiLoginTokens`. |
| `resend2fa` (*Resend2fa*) | `POST core/v1/auth2/2fa/resend`, body `{ "token" }` | `token` from the 2FA challenge |
| `logout` (*Logout*) | `POST core/v1/auth2/logout`, body `{ "deviceId" }` | |
| `enable2fa` (*Enable2fa*) | `POST core/v1/auth2/2fa/enable`, body `{ "phoneNumber" }` | |
| `disable2fa` (*Disable2fa*) | `POST core/v1/auth2/2fa/disable` | No body |
| `requestRegistration` (*RequestRegistration*) | `POST core/v1/auth2/login/create`, header `X-Spond-Timezone`, body `ApiRegistrationRequest` `{ registrationMethod, phoneNumber, email, password, firstName, lastName, locale, intMarketing, extMarketing, registrationSource }` | Returns `ApiRegistrationResponse` `{ method, phoneNumber, primaryEmail, verification }` |
| `verifyRegistration` (*VerifyRegistration*) | `POST core/v1/auth2/login/verify`, header `X-Spond-Timezone`; body carries the identifier (`email`/`phoneNumber`), `verification` (`{ token, code }`, `ApiVerification`) or `verificationCode`, and `twoFactorVerification` | Exact nesting *unverified* |
| `requestPasswordReset` (*RequestPasswordReset*) | `POST core/v1/auth2/login/reset`, body `{ "email" }` or `{ "phoneNumber" }` | Response holds a `verification` token |
| `verifyPasswordReset` (*VerifyPasswordReset*) | `PUT core/v1/auth2/login`, body with `password`, `resetCode` / `code`, `token`, `verification`, `twoFactorVerification` | Exact nesting *unverified* |
| `changePasswordByPassword` (*ChangePassword*) | `POST core/v1/auth2/login/update`, body `{ "currentPassword", "password" }` | |
| `changePasswordByToken` (*ChangePassword*) | `POST core/v1/auth2/login/update`, body `{ "passwordToken", "password" }` | `passwordToken` from `ApiLoginTokens` |

### Profile (`ProfileApi`)

All the "update my …" calls are `PUT core/v1/profile` with a partial body:

| Call | Body |
|------|------|
| `submitMyProfile` (*SubmitMyProfile*) | `{ firstName, lastName, imageUrl, timezone, dateOfBirth }` |
| `updateMyName` (*UpdateMyName*) | `{ firstName, lastName }` (the app rejects an empty name) |
| `updateMyBirthday` (*UpdateMyBirthday*) | `{ dateOfBirth }` |
| `updateMyCountry` (*UpdateMyCountry*) | `{ countryCode }`, an ISO alpha-3 code |
| `updateMyGender` (*UpdateMyGender*) | `{ gender }` |
| `updateMyImage` (*UpdateMyImage*) | `{ imageUrl }` |
| `updateMyTimezone` (*UpdateProfile*) | `{ timezone }` |
| `acceptTermsAndPrivacy` (*UpdateProfile*) | `{ tosVersion: <int> }` |
| `updateGlobalPushSettings` (*UpdateGlobalPushSettings*) | `{ preferences: { globalPushPreferences: PushSettings } }` |
| `updateGroupPushSettings` (*UpdateGroupPushSettings*) | `{ preferences: { groupPushPreferences: { <groupId or subgroup key>: PushSettings } } }` |
| `toggleGroupPushSettings` (*ToggleGroupPushSettings*) | `{ preferences: { groupPushPreferences: { …: { pushesDisabled } } } }` |
| `clearGroupPushSettings` (*ClearGroupPushSettings*) | `{ preferences: { groupPushPreferences: { …: { clearSettings: true } } } }` |
| `updateOptionalSettings` (*UpdateOptionalSettings*) | `{ preferences: { optionalSettings: { ecbCommunicationConsentGiven } } }` |
| `toggleTargetedAds` (*ToggleTargetedAds*) | `{ preferences: { targetedAdsDisabled } }` |
| `toggleCashbackPromotions` (*ToggleCashbackPromotions*) | `{ preferences: { cashbackPromoDisabled } }` |
| `togglePartnerPromotions` (*TogglePartnerPromotions*) | `{ preferences: { partnerPromoDisabled } }` |

`PushSettings` keys: `pushesDisabled`, `acceptPushDisabled`,
`declinePushDisabled`, `acceptTaskPushDisabled`, `declineTaskPushDisabled`,
`invitationPushDisabled`, `postPushDisabled`, `reminderPushDisabled`,
`scheduledPrealertPushDisabled`, `scheduledSentPushDisabled`,
`bonusContributePushDisabled`, `bonusAchievementPushDisabled`,
`matchNotificationsPushDisabled`, … (all booleans). Where exactly
`preferences` nests in the JSON is inferred from the key order.

Other profile calls:

| Call | Request | Returns / notes |
|------|---------|-----------------|
| `updateMyImage` with no image (*DeleteMyImage*) | `DELETE core/v1/profile/image` | |
| `deleteProfile` (*DeleteProfile*) | `POST core/v1/profile/{profileId}/delete`, body `{ "password" }` | Deletes the account |
| `mergeProfile` (*MergeProfile*) | `POST core/v1/profile/merge`, body `{ "profileId", "updateCode" }` | Merge a duplicate account |
| `putCmpConsent` (*PutCmpConsent*) | `PUT core/v1/profile/cmp-consent`, body `{ "consent": bool }` | Ad consent (CMP) |
| `getMyProfileHash` (*GetMyProfileHash*) | `GET core/v1/profile/hash` | `{ "androidUserHash" }` (Intercom identity) |
| `getFundraisingAvailability` (*GetFundraisingAvailability*) | `GET core/v1/profile/fundraising` | `FundraisingAvailability` `{ campaigns, campaignsAvailable }` |
| `addContactPoint` (*AddContactPoint*) | `POST core/v1/contactPoints/request`, body `{ "email" }` or `{ "phoneNumber" }` (by `ContactPointType`) | Sends a verification code |
| `verifyContactPoint` (*VerifyContactPoint*) | `POST core/v1/contactPoints/verify`, body with the contact point, `verification` `{ token, code }` / `verificationCode` | |
| `markContactPointAsPrimary` (*MarkContactPointAsPrimary*) | `POST core/v1/contactPoints/primary`, body: the contact point | |
| `removeContactPoint` (*RemoveContactPoint*) | `POST core/v1/contactPoints/remove`, body: the contact point | |

`ApiContactPoint`: `emailAddress`, `phoneNumber`, `primary`, `verified`,
`verification` (`{ token, phoneNumber }`).

### Groups: lifecycle, invitations and settings (`GroupApi`)

| Call | Request | Body / returns |
|------|---------|----------------|
| `createGroup` (*CreateGroup*) | `POST core/v1/group` | `{ type, name, description, welcomeMessage, activity, chatAgeLimit, contactInfoHidden }` → `ApiGroup` |
| `changeGroup` (*ChangeGroup*) | `PUT core/v1/group/{groupId}` | Changed keys of `{ type, name, description, welcomeMessage, imageUrl, activity, contactPersonId, eventVisibility, chatAgeLimit, contactInfoHidden }` |
| `changeMemberPermissions` (*ChangeMemberRole*) | `PUT core/v1/group/{groupId}` | `{ memberPermissions: [permission…] }` |
| `changeGuardianPermissions` (*ChangeGuardianRole*) | `PUT core/v1/group/{groupId}` | `{ guardianPermissions: [permission…] }` |
| `deleteGroup` (*DeleteGroup*) | `DELETE core/v1/group/{groupId}` | |
| `getGroupLeaveInfo` (*getGroupLeaveInfo*) | `GET core/v1/group/{groupId}/leaveInfo` | `GroupLeaveInfo` `{ groupId, memberId, groupContact, guardianOfMembers[{memberId, soleGuardian}], soleHostSponds[{spondId, spondName}], soleHostSeries[{spondId, seriesId, spondName}] }`: what blocks or follows from leaving |
| `leaveGroup` (*LeaveGroup*) | `PUT core/v1/group/{groupId}/leave` | |
| `getGroupInvites` (*getGroupInvites*) | `GET core/v1/groups/group-invitations` | list of `ApiGroupInvite` `{ id, name, type, description, imageUrl, activity, countryCode, contactPerson, contactInfoHidden, roles, organizationType }` |
| `acceptGroupInvite` (*AcceptGroupInvite*) | `PUT core/v1/group/{groupId}/acceptInvite?shareContactInfo=true` | |
| `declineGroupInvite` (*DeclineGroupInvite*) | `PUT core/v1/group/{groupId}/declineInvite` | |
| `shareContactInfo` (*ShareContactInfo*) | `PUT core/v1/group/{groupId}/shareContactInfo` | `{ value: bool }` |
| `getGroupActivities` (*getGroupActivities*) | `GET core/v1/groups/group-activities` | The list of sports/activities a group can have |
| `addFavorite` (*AddGroupFavorite*) | `PUT core/v1/groups/favorites/{groupId}` or `…/{groupId}/{subGroupId}` | → `ApiGroupFavorite` `{ groupId, timestamp, subGroupId }` |
| `deleteFavorite` (*RemoveGroupFavorite*) | `DELETE core/v1/groups/favorites/{groupId}` or `…/{groupId}/{subGroupId}` | |
| `registerEmailForGroupCreationDisabled` (*RegisterEmailForGroupCreationDisabled*) | `POST core/v1/subscribe/2` | `{ email }`: "tell me when I can create groups" |

**Admin roles**

| Call | Request | Body |
|------|---------|------|
| `addGroupRole` (*AddGroupRole*) | `POST core/v1/group/{groupId}/roles` | `{ name, permissions: [...] }` |
| `changeGroupRole` (*ChangeGroupRole*) | `PUT core/v1/group/{groupId}/roles/{roleId}` | `{ name, permissions }` |
| `deleteGroupRole` (*DeleteGroupRole*) | `DELETE core/v1/group/{groupId}/roles/{roleId}` | |

Permission names are listed under [Admin](#who-is-an-admin-and-with-which-rights).

**Subgroups**

| Call | Request | Body |
|------|---------|------|
| `createSubgroup` (*CreateSubgroup*) | `POST core/v1/group/{groupId}/subgroups` | `{ name, color, imageUrl, members: [membershipId…] }` |
| `changeSubgroup` (*ChangeSubgroup*) | `PUT core/v1/group/{groupId}/subgroups/{subGroupId}` | Changed keys of `{ name, color, imageUrl }` |
| `deleteSubgroup` (*DeleteSubgroup*) | `DELETE core/v1/group/{groupId}/subgroups/{subGroupId}` | |

**Member fields**

| Call | Request | Body |
|------|---------|------|
| `createMemberFieldDef` (*CreateMemberFieldDef*) | `POST core/v1/group/{groupId}/fieldDefs` | `EditedCustomMemberFieldDef` (`name`, `type`, `memberFieldOptions`, `visibility`, `permission`, `required`, …) |
| `changeMemberFieldDef` (*ChangeMemberFieldDef*) | `PUT core/v1/group/{groupId}/fieldDefs/{fieldId}` | same |
| `changeMemberFieldDef` for a built-in field | `PUT core/v1/group/{groupId}/fieldDefs/permissions/{field}` | `{ permission }`; `field` is one of `field_member_dob`, `field_member_address`, `field_member_gender`, `field_member_nationality`, `field_member_country`, `field_member_guardian` |
| `deleteMemberFieldDef` (*DeleteMemberFieldDef*) | `DELETE core/v1/group/{groupId}/fieldDefs/{fieldId}` | |
| `reorderMemberFieldDefs` (*ReorderMemberFieldDefs*) | `PUT core/v1/group/{groupId}/fieldDefs/reorder` | JSON array of field ids in the new order |

Built-in field settings come back in `group.defaultFields`
(`ApiDefaultMemberFieldsDef`: `field_member_country`, `field_member_nationality`,
`field_member_gender`, `field_member_address`, `field_member_dob`,
`field_member_guardian`, `field_member_spond_id`, each
`{ visibility, permission, locked, required, enabled }`).

**Single member fields.** Besides the full
[`POST …/member`](#change-a-membership-groupapiaddorupdatemembership), a member's
own profile screen updates one field at a time:

| Call | Request | Body |
|------|---------|------|
| `updateMemberImage` (*ChangeMembershipImage*) | `PUT core/v1/group/{groupId}/member/{membershipId}` | `{ imageUrl }` |
| `updateMemberAddress` (*UpdateMemberAddress*) | `PUT core/v1/group/{groupId}/member/{membershipId}/address` | `Address` `{ street, city, zip, state }` |
| `updateMemberCountry` (*UpdateMemberCountry*) | `PUT …/member/{membershipId}/country` | `{ value }` |
| `updateMemberNationality` (*UpdateMemberNationality*) | `PUT …/member/{membershipId}/nationality` | `{ value }` |
| `updateMemberGender` (*UpdateMemberGender*) | `PUT …/member/{membershipId}/gender` | `{ value }` (`M`/`F`/`O`) |
| `updateMemberDob` (*UpdateMemberDob*) | `PUT …/member/{membershipId}/field/field_member_dob` | `{ value: "YYYY-MM-DD" }` |
| `updateMemberField` (*UpdateMemberField*) | `PUT …/member/{membershipId}/field/{fieldId}` | `{ value }` |

**Other member management**

| Call | Request | Body / returns |
|------|---------|----------------|
| `markMembersProcessed` (*MarkMembersProcessed*) | `POST core/v1/group/{groupId}/members/markProcessed` | JSON array of membership ids (clears `needsProcessing`) |
| `exportMembers` | `GET core/v1/group/{groupId}/exportMembers` | File download (spreadsheet) |
| `exportAttendance` | `POST core/v1/group/{groupId}/exportAttendance` | Body: the selected ids; file download |
| `exportMatchResult` | `POST core/v1/group/{groupId}/exportMatches` | Body: the selected ids; file download |
| `sendWebLinkToImportMembers` (*SendWebLinkToImportMembers*) | `POST core/v1/group/{groupId}/sendWebLink` | `{ email }`: e-mails a link to the web member import |

**Join links and sign-up** (the public "join group" flow)

| Call | Request | Body / returns |
|------|---------|----------------|
| `enableGroupLink` (*EnableGroupLink*) | `PUT core/v1/group/{groupId}/signupCode` | Turns the join link on |
| `disableGroupLink` | `DELETE core/v1/group/{groupId}/signupCode` | |
| `generateGroupSignupLink` (*GenerateGroupSignupLink*) | `GET core/v1/group/{groupId}/signupLink` | `{ url }` |
| `getSharedGroupInfo` (*getSharedGroupInfo*) | `GET core/v1/groupSignupCode/{code}` | `ApiSharedGroupInfo` `{ type, name, welcomeMessage, imageUrl, activity, signupUrl, signupCode, countryCode, canSetDob, canSetAddress, canSetGender, canSetNationality, canSetCountry, dobRequired, addressRequired, genderRequired, nationalityRequired, countryRequired, contactInfoHidden, addressFormat, editableFields, organizationType }` |
| `findGroupSignupMemberMatches` (*FindGroupSignupMemberMatches*) | `GET core/v1/groupSignupCode/{code}/findMembers?name=…` | list of `{ name, guardians, groupMembershipId }`: existing memberships the new person may be |
| `signupGroup` (*SignupGroup*) | `POST core/v1/groupSignupCode/{code}/requestMembership` | `{ groupMembershipId?, profile: { firstName, lastName, phoneNumber, email }, guardians: [same], address, dateOfBirth, country, nationality, gender, fieldValues, contactInfoHidden }`. Creates an `ApiMembershipRequest` |
| `getGroupSignups` (*getGroupSignups*) | `GET core/v1/groupSignupCode/myRequests` | list of `ApiGroupSignup` `{ id, name, signupCode, signupUrl, imageUrl, activity, memberName }`: your pending requests |
| `cancelGroupSignup` (*CancelGroupSignup*) | `POST core/v1/groupSignupCode/{code}/{requestId}/cancelRequest` | |

### Clubs (`ClubApi`)

| Call | Request | Body / returns |
|------|---------|----------------|
| `createClub` (*CreateClub*) | `POST core/v1/clubs` | `{ type, name, description, welcomeMessage, activity, chatAgeLimit, contactInfoHidden }` |
| `createLinkedGroup` (*CreateLinkedGroup*) | `POST core/v1/clubs/{clubId}/groups` | same keys; creates a group inside the club |
| `addExistingClubMembersToLinkedGroup` (*AddExistingClubMembersToLinkedGroup*) | `POST core/v1/clubs/{clubId}/groups/{groupId}/members/existing` | list of `ApiExistingClubMemberPlacement` `{ clubMembershipId, respondent, subGroupIds, roleIds }` → `{ added, skipped: [{ clubMembershipId, reason }] }` |

### Group files (`GroupResourceApi`)

A group's "Files" tab: folders, links and uploaded files.

| Call | Request | Body / returns |
|------|---------|----------------|
| `getFolder` (*GetGroupFolder*) | `GET core/v1/group/{groupId}/resources/folders/{folderId}` | `ApiGroupFolder` `{ id, groupId, parentId, name, creatorId, createdAt, visibility, itemCount, systemFolder, folders, resources, parentNames }` |
| `searchResources` (*SearchGroupResources*) | `GET core/v1/group/{groupId}/resources/search?name=…&folder=…` | `{ folders, resources }` |
| `createFolder` (*CreateGroupFolder*) | `POST core/v1/group/{groupId}/resources/folders/{parentFolderId}` | `{ name, visibility }` |
| `createLink` (*CreateGroupLink*) | `POST core/v1/group/{groupId}/resources` | `{ name, url, parentId }` |
| `changeResource` (*ChangeGroupResource*) | `PUT core/v1/group/{groupId}/resources/{resourceId}` | `{ name, visibility }` |
| `moveResources` (*MoveGroupResources*) | `PUT core/v1/group/{groupId}/resources/folders/{targetFolderId}` | JSON array of resource ids |
| `deleteResources` (*DeleteGroupResources*) | `DELETE core/v1/group/{groupId}/resources` | JSON array of resource ids (a DELETE with a body) |

`ApiGroupFile`: `id`, `groupId`, `parentId`, `name`, `type`, `creatorId`,
`createdAt`, `url`, `size`, `mediaType`, `parentNames`. Uploading the file
itself goes through [storage](#file-storage-storageapi).

### Arrangements: create, edit, delete (`SpondApi`)

**Create and edit.** The body is built by `SpondApi.toJsonObject(EditedSpond, isNew)`.
For an edit, only changed keys are sent.

| Call | Request |
|------|---------|
| `createSpond` (*CreateSpond*) | `POST core/v1/sponds` |
| `updateSpond` (*UpdateSpond*) | `POST core/v1/sponds/{spondId}` with headers `X-Spond-ClearResponses: true\|false` (reset everyone's answers) and `X-Spond-SendUpdate: true\|false` (notify recipients) |

Body keys: `heading`, `description`, `type`, `startTimestamp`,
`endTimestamp`, `openEnded`, `meetupPrior`, `rsvpDate`, `inviteTime`,
`autoAccept`, `maxAccepted`, `participantsHidden`, `commentsDisabled`,
`visibility`, `autoReminderType`, `picture` (an `https://` URL),
`availabilityRequestId`, `publicMetadata { activityType, skillLevel, ageGroup }`,
`location` (see `ApiSpond.location`),
`recipients { group: { id, subGroups, groupMembers, guardians }, profiles: [{ profileId, phoneNumber, email }] }`,
`attachments [{ id, title, type, media, mediaType }]`, `owners`,
`payment { accountId, accountName, provider, currency, total, amount, payerPaysFee }`,
`tasks { openTasks, assignedTasks }`,
`matchInfo { type, teamName, opponentName, teamColour, opponentColour }`.

Other single-purpose `POST core/v1/sponds/{spondId}` calls, same endpoint with
one key:

| Call | Body |
|------|------|
| `sendInvitation` (*InviteSpond*) | `{ inviteTime }`: send a scheduled invitation now / reschedule |
| `disableAutoReminder` (*DisableAutoReminder*) | `{ autoReminderType: "DISABLED" }` |
| `updateHosts` (*UpdateSpondHosts*) | `{ id, owners: [...] }` |
| `updatePaymentDetails` (*UpdateSpondPayment*) | `{ payment: { … amount … } }` (`ApiEventPaymentDetails`) |
| `addTask` (*AddSpondTask*) | `{ tasks: … }` |

| Call | Request | Notes |
|------|---------|-------|
| `deleteSpond` (*DeleteSpond*) | `DELETE core/v1/sponds/{spondId}?quiet=true&reason=…` | `quiet`: don't notify. `reason` becomes the cancel message. |
| `hideCancelledSpond` (*HideCancelledSpond*) | `PUT core/v1/sponds/{spondId}/hide` | |
| `muteSpond` (*MuteSpond*) | `PUT core/v1/sponds/{spondId}/muted`, body `{ value: bool }` | Mute notifications |
| `addRecipients` (*AddSpondRecipients*) | `POST core/v1/sponds/{spondId}/recipients`, body `{ group: { id, subGroups, groupMembers, guardians }, profiles }` | Invite more people |
| `removeRecipient` (*RemoveSpondRecipient*) | `DELETE core/v1/sponds/{spondId}/recipients/{recipientId}` | |
| `acceptHost` (*AcceptSpondHost*) | `POST core/v1/sponds/{spondId}/acceptHost` or `…/acceptHost/{behalfOfId}` | Accept being a host |
| `declineHost` (*DeclineSpondHost*) | `POST core/v1/sponds/{spondId}/declineHost` or `…/declineHost/{behalfOfId}` | |
| `joinPaymentSpond` (*JoinPaymentSpond*) | `PUT core/v1/sponds/{spondId}/responses/{recipientId}`, header `X-Spond-SkipPayment: true\|false`, body `{ accepted: true }` | Accepting a paid arrangement (see [Answer an arrangement](#answer-an-arrangement-spondapirespondtospond)) |
| `registerSpondAttendance` (*RegisterSpondAttendance*) | `POST core/v1/sponds/{spondId}/attendance`, body: map recipient id → attended | Fills `responses.registeredAttendance` |
| `sendSpondNag` (*SendSpondNag*) | `POST core/v1/sponds/{spondId}/nag?tasksOnly=true`, body `{ text, recipients: [...] }` | Reminder to people who have not answered |
| `generateSpondNagLink` (*GenerateSpondNagLink*) | `GET core/v1/sponds/{spondId}/nagLink` | `{ url }` to share |
| `exportSpond` | `GET core/v1/sponds/{spondId}/export` | File download |
| `getIdByCode` (*GetSpondIdByCode*) | `GET core/v1/sponds/code/{code}` | `{ spondId }`: resolves a share code |
| `getArChildEvents` (*GetARChildEvents*) | `GET core/v1/sponds/{spondId}/children?includeComments=true&addProfileInfo=true` | Children of an availability request: `{ events, recipientSelections, hostSelections, hasUnselectedBehalfOf }` |
| `getSpondSeenRecipientIds` (*GetSpondSeen*) | `GET core/v1/seen/spond/{spondId}` | Who has opened it |
| `markSpondsAsSeen` (*MarkSpondsAsSeen*) | `POST core/v1/seen/sponds`, body: JSON array of ids | |
| `getPaymentDetails` (*GetSpondPaymentDetails*) | `GET core/v1/sponds/{spondId}/paymentDetails` | `ApiEventPaymentDetails` `{ accountId, accountName, provider, currency, amount, total, payerPaysFee }` |
| `getSpondPaymentReceipts` (*GetSpondPaymentReceipts*) | `GET core/v1/payments/spond?spondId=…` | list of `ApiPaymentReceipt` |
| `bulkInviteGroupSponds` (*BulkInviteGroupSponds*) | `POST core/v1/group/{groupId}/bulkInvite`, body `[{ memberId, markProcessed, spondIds, seriesIds }]` | Invite a new member to existing arrangements (pairs with `getMissingSponds`) |
| `sendWebLinkToScheduleEvents` (*SendWebLinkToScheduleEvents*) | `POST core/v1/link/bulkSpond`, body `{ email, groupId, subGroupId }` | E-mails a link to the web bulk scheduler |
| `searchSpondAttendances` (*SearchSpondAttendances*) | `GET core/v1/search/groups/{groupId}/sponds` + `keyword`, `subGroupId`, `fromTimestamp`, `toTimestamp`, `expired`, `includeDeleted`, `includeScheduled`, `excludeSubGroupSponds`, `excludeSeriesSponds`, `excludeExpired`, `matchesOnly` | list of `ApiSpondAttendance` `{ spondId, name, startTimestamp, ownerFirstName, ownerLastName }`; used to pick arrangements for attendance export |

**Public sign-ups** (arrangements with `visibility: PUBLIC`)

| Call | Request | Body / returns |
|------|---------|----------------|
| `getSignups` (*GetSpondSignups*) | `GET core/v1/sponds/{spondId}/signups?page=…&pageSize=…&attendanceResponse=…` | `{ page, pageSize, hasMore, total, results: [ApiSpondSignup] }`; `ApiSpondSignup` `{ signupId, profileId, email, phoneNumber, dateOfBirth, attendanceResponse, invitedToGroup, firstName, lastName, imageUrl, createdAt, attendeeType, guardian }` |
| `removeSignupFromEvent` (*RemoveSpondSignup*) | `DELETE core/v1/sponds/{spondId}/signups/{signupId}` | |
| `inviteSignupsToGroup` (*InviteSpondSignupsToGroup*) | `POST core/v1/sponds/{spondId}/signups/invite` | `{ signupIds }` → `{ results: [{ signupId, groupId, status, invitedToGroup }] }` |
| `answerMyPublicSignup` (*AnswerMyPublicSignup*) | `POST core/v1/public/events/{spondId}/attendance/response` | `{ attendanceResponse: "ACCEPTED" \| "DECLINED" }` |

### Arrangement comments and reactions

Same shape for arrangements (`SpondApi`) and posts (`PostApi`); replace
`sponds` with `posts`.

| Call | Request | Body |
|------|---------|------|
| `createComment` (*CreateSpondComment* / *CreatePostComment*) | `POST core/v1/sponds/{id}/comments`; a reply: `POST core/v1/sponds/{id}/comments/{parentCommentId}` | `{ text, image?: { url, width, height }, hostsOnly?, notifications?: { mentions: [{ id, type }] } }` |
| `editComment` (*EditSpondComment* / *EditPostComment*) | `PUT core/v1/sponds/{id}/comments/{commentId}` | `{ text, notifications: { mentions } }` |
| `deleteComment` (*DeleteSpondComment* / *DeletePostComment*) | `DELETE core/v1/sponds/{id}/comments/{commentId}` | |
| `changeCommentReaction` set (*SetSpondCommentReaction*) | `PUT core/v1/sponds/{id}/comments/{commentId}/reaction` | `{ value: "<emoji>" }` |
| `changeCommentReaction` remove (*RemoveSpondCommentReaction*) | `DELETE core/v1/sponds/{id}/comments/{commentId}/reaction` | |

Mentions in comments use `type: "MEMBERSHIP"` and the U+E001 placeholder in
`text`, as in [chat](#messages-chatapi), but sit under `notifications.mentions`.
`hostsOnly` makes the comment visible to hosts only. A received comment
(`ApiComment`) carries the same `notifications` and `image`.

### Arrangement tasks (`SpondApi`)

Tasks ("bring cake", "drive") hang off an arrangement. `ApiSpondTask`:
`id`, `name`, `type`, `description`, `adultsOnly`, `limit`, `remaining`,
`unanswered`, `accepted`, `declined` (the last three: `[{ id, message, answererId }]`).

| Call | Request | Body |
|------|---------|------|
| `addTask` (*AddSpondTask*) | `POST core/v1/sponds/{spondId}` | `{ tasks: { openTasks, assignedTasks } }` |
| `updateTask` (*UpdateSpondTask*) | `PUT core/v1/sponds/{spondId}/tasks/{taskId}` | `EditedSpondTask`: `{ id, name, description, type, adultsOnly, limit, saveTemplate, assignments: { update / delete / remove, memberIds, profiles } }` |
| `removeTasks` (*RemoveSpondTasks*) | `PUT core/v1/sponds/{spondId}/tasks/remove` | JSON array of task ids |
| `takeTask` (*TakeSpondTask*) | `POST core/v1/sponds/{spondId}/tasks/{taskId}/assignments` | Volunteer for an open task |
| `addTaskAssignments` (*AddSpondTaskAssignments*) | `PUT core/v1/sponds/{spondId}/tasks/{taskId}/assignments` | `{ memberIds, profiles }` |
| `removeTaskAssignments` (*RemoveSpondTaskAssignments*) | `PUT core/v1/sponds/{spondId}/tasks/{taskId}/assignments/remove` | JSON array of ids |
| `respondToSpondTask` (*RespondToSpondTask*) | `PUT core/v1/sponds/{spondId}/tasks/{taskId}/assignments/{recipientId}` | `{ accepted, declineMessage? }` |

Task templates, per group:

| Call | Request | Body |
|------|---------|------|
| `getTaskTemplates` (*GetTaskTemplates*) | `GET core/v1/tasks/templates/{groupId}?max=500&offset=0&type=…` | list of `ApiTaskTemplate` `{ id, type, name, description, adultsOnly, limit }` |
| `createTaskTemplate` (*CreateTaskTemplate*) | `POST core/v1/tasks/templates/{groupId}` | `ApiTaskTemplate` |
| `updateTaskTemplate` (*UpdateTaskTemplate*) | `PUT core/v1/tasks/templates/{groupId}/{templateId}` | `ApiTaskTemplate` |
| `deleteTaskTemplates` (*DeleteTaskTemplates*) | `PUT core/v1/tasks/templates/{groupId}/remove` | JSON array of template ids |

### Matches (`SpondApi`, `LineupApi`)

An arrangement with `matchInfo` is a match.

| Call | Request | Body / returns |
|------|---------|----------------|
| `updateMatchScore` (*UpdateMatchScore*) | `POST core/v1/sponds/{spondId}/matchUpdate` | `{ teamScore, opponentScore, scoresPublic, scoresFinal }` |
| `clearMatchScore` (*ClearMatchScore*) | `POST core/v1/sponds/{spondId}/matchUpdate` | `{ scoresSet: false }` |
| `generateMatchResultImage` (*GenerateMatchResultImage*) | `PUT core/v1/sponds/{spondId}/shareMatchResults?version={n}` | `{ previewImageUrl, shareableImageUrl, version }` |
| `getSpotlightPlayers` (*GetSpotlightPlayers*) | `GET core/v1/sponds/{spondId}/spotlight-players` | `{ spondId, title, postId, winners: [{ name, entityId }] }` (player-of-the-match vote) |
| `getMatchPollPosts` (*GetMatchPollPosts*) | `GET core/v1/posts?spondId={spondId}` | The poll posts tied to a match |
| `getMatchConfirmationRequest` (*GetMatchConfirmationRequest*) | `GET core/v1/sponds/{spondId}/matchConfirmationRequest` | `{ status, recipients, lastSent, requestingCoach }` |
| `sendMatchConfirmationRequest` (*SendMatchConfirmationRequest*) | `POST core/v1/sponds/{spondId}/matchConfirmationRequest/send` | Ask the other team / referee to confirm the result |
| `addMatchConfirmationRequestRecipient` (*AddMatchConfirmationRequestRecipient*) | `POST core/v1/sponds/{spondId}/matchConfirmationRequest/recipient` | `EditedMatchConfirmationRecipient` (`role`, `firstName`, `lastName`, `emailAddress`, `phoneNumber`) |
| `updateMatchConfirmationRequestRecipient` (*UpdateMatchConfirmationRequestRecipient*) | `PUT core/v1/sponds/matchConfirmationRequest/recipient/{recipientId}` | same |
| `removeMatchConfirmationRequestRecipient` (*RemoveMatchConfirmationRequestRecipient*) | `DELETE core/v1/sponds/matchConfirmationRequest/recipient/{recipientId}` | |

Line-ups (`LineupApi`):

| Call | Request | Body / returns |
|------|---------|----------------|
| `getLineups` (*GetLineups*) | `GET core/v1/sponds/{spondId}/lineups` | list of `Lineup` `{ id, formationId, spondId, activityType, name, visibility, players, substitutes }`; a player is `{ playerName, membershipId, profileId, x, y }` |
| `addNewLineup` (*AddNewLineup*) | `POST core/v1/sponds/{spondId}/lineups` | `LineupRequest` `{ activityType, formationId, visibility, name, players, substitutes }` |
| `getLineup` (*GetLineup*) | `GET core/v1/lineups/{lineupId}` | `Lineup` |
| `updateLineup` (*UpdateLineup*) | `PUT core/v1/lineups/{lineupId}` | `LineupRequest` |
| `deleteLineup` (*DeleteLineup*) | `DELETE core/v1/lineups/{lineupId}` | |
| `getLineupFormations` (*GetLineupFormations*) | `GET core/v1/lineups/formations` | list of `{ id, name, teamSize, positions: [{ name, x, y }] }` |
| `getLineupAssets` (*GetLineupAssets*) | `GET core/v1/lineups/assets` | `{ backgrounds: [{ activityType, lightModeBaseUrl, darkModeBaseUrl }] }` |

### Recurring arrangements (`SeriesApi`)

A repeating arrangement is a **series**; each occurrence is an arrangement with
`seriesId` and `seriesOrdinal`. `ApiSeries`: `id`, `groupId`, `heading`,
`description`, `picture`, `interval`, `startTime`, `lastStart`, `timeZone`,
`duration`, `inviteOffset`, `rsvpOffset`, `maxAccepted`, `meetupPrior`,
`autoAccept`, `inviteMuted`, `participantsHidden`, `commentsDisabled`,
`subGroupIds`, `ownerIds`, `autoReminderType`, `visibility`, `payment`,
`location`, `sponds`.

| Call | Request | Body |
|------|---------|------|
| `createSeries` (*CreateSeries*) | `POST core/v1/series?includeSponds=true` | `SeriesApi.toJsonObject`: `{ timeZone, groupId, heading, description, interval, startTime, lastStart, duration, autoAccept, maxAccepted, inviteMuted, inviteOffset, rsvpOffset, meetupPrior, participantsHidden, commentsDisabled, visibility, autoReminderType, ownerIds, subGroupIds, respondents, picture, location, payment }` |
| `getSeries` (*GetSeries*) | `GET core/v1/series/{seriesId}?includeSponds=true` | → `ApiSeries` |
| `splitSeries` (*SplitSeries*) | `POST core/v1/series/{seriesId}/split?includeSponds=true`, headers `X-Spond-ClearResponses`, `X-Spond-SendUpdate` | Same body plus `ordinal`: edit "this and following" occurrences |
| `deleteSeries` (*DeleteSeries*) | `DELETE core/v1/series/{seriesId}?fromOrdinal=N&quiet=true&deleteReason=…` | |
| `getPaymentDetails` (*GetSeriesPaymentDetails*) | `GET core/v1/series/{seriesId}/paymentDetails` | |
| `muteSeries` (*MuteSeries* / *UnmuteSeries*) | `PUT` / `DELETE core/v1/series/{seriesId}/muteInvite` | |
| `addRespondents` (*AddSeriesRespondents*) | `POST core/v1/series/{seriesId}/respondents/add` | `{ fromOrdinal, memberIds }` |
| `removeRespondents` (*RemoveSeriesRespondents*) | `POST core/v1/series/{seriesId}/respondents/remove` | `{ fromOrdinal, memberIds }` |
| `addHosts` (*AddSeriesHosts*) | `POST core/v1/series/{seriesId}/respondents/add` | `{ fromOrdinal, ownerIds }` |
| `removeHosts` (*RemoveSeriesHosts*) | `POST core/v1/series/{seriesId}/respondents/remove` | `{ fromOrdinal, ownerIds }` |

### Posts: create, edit, read state (`PostApi`)

| Call | Request | Body |
|------|---------|------|
| `createPlainPost` (*CreatePlainPost*) | `POST core/v1/posts` | `{ type: "PLAIN", groupId, subGroupIds, title, body, visibility, commentsDisabled, media: [{ type, url, thumbnailUrl, uploadId, width, height }], attachments: [{ id, title, type, media, mediaType }] }` |
| `updatePlainPost` (*UpdatePlainPost*) | `PUT core/v1/posts/{postId}` | same keys |
| `createPollPost` (*CreatePollPost*) | `POST core/v1/posts` | `{ type: "POLL", groupId, subGroupIds, commentsDisabled, spondId?, poll: { type, question, description, dueBy, multipleChoice, hideVotes, options: [{ id, text, entityId }] } }` |
| `updatePollPost` (*UpdatePollPost*) | `PUT core/v1/posts/{postId}` | same keys |
| `createPaymentPost` (*CreatePaymentPost*) | `POST core/v1/posts` | `{ type: "PAYMENT", groupId, subGroupIds, commentsDisabled, attachments, payment: { provider: "STRIPE", title, description, dueBy, mandatory, payerPaysFee, accountId, basePrice, total, optionalProducts: [{ id, name, price, total, maxQuantity, deleted }], respondents: [{ memberId }] } }` |
| `updatePaymentPost` (*UpdatePaymentPost*) | `PUT core/v1/posts/{postId}` | same keys |
| `updatePostSubgroups` (*UpdatePostSubgroups*) | `PUT core/v1/posts/{postId}`, header `X-Spond-SendUpdate` | `{ subGroupIds }` |
| `deletePost` (*DeletePost*) | `DELETE core/v1/posts/{postId}` | |
| `mutePost` / `unmutePost` (*MutePost*) | `PUT core/v1/posts/{postId}/muted` | `{ value: true }` / `{ value: false }` |
| `reportPost` (*ReportPost*) | `POST core/v1/posts/{postId}/report` | `{ reason, recipients }` |
| `changePostReaction` set (*SetPostReaction*) | `PUT core/v1/posts/{postId}/reaction` | `{ value: "<emoji>" }` |
| `changePostReaction` remove (*RemovePostReaction*) | `DELETE core/v1/posts/{postId}/reaction` | |
| `sendPostNag` (*SendPostNag*) | `POST core/v1/posts/{postId}/nag` | `{ text, recipients }`: remind people who have not voted / paid |
| `generatePostNagLink` (*GeneratePostNagLink*) | `GET core/v1/posts/{postId}/nagLink` | → `{ url }` |
| `getPostSeen` (*GetPostSeen*) | `GET core/v1/seen/posts?id={postId}` | → `{ seenBy }` |
| `markPostsAsSeen` (*MarkPostsAsSeen*) | `POST core/v1/seen/posts` | JSON array of post ids |
| `markPostsAsRead` (*MarkPostsAsRead*) | `POST core/v1/posts/markAsRead` | JSON array of post ids (clears `unread`) |
| `markAllPostsAsChecked` (*MarkAllPostsAsChecked*) | `PUT core/v1/posts/checked` | Clears the posts badge |
| `getBadgeCounts` (*GetPostBadgeCounts*) | `GET core/v1/posts/badge` | Counts in the `X-Spond-*BadgeCount` response headers (see [List posts](#list-posts)) |

Comments and reactions on comments: see
[Arrangement comments and reactions](#arrangement-comments-and-reactions).

### Polls (`PostApi`)

`ApiPoll`: `type`, `question`, `description`, `dueBy`, `spondId`,
`multipleChoice`, `expired`, `hideVotes`, `options` (`[{ id, text, entityId, votes }]`),
`blankVotes`, `behalfOfIds`.

| Call | Request | Body |
|------|---------|------|
| `votePollOption` (*VotePollOption*) | `PUT core/v1/posts/{postId}/poll/vote/{optionId}/{behalfOfId}` | `{ value: true }` to vote, `false` to withdraw |
| `votePollBlank` (*VotePollBlank*) | `PUT core/v1/posts/{postId}/poll/vote/blank/{behalfOfId}` | `{ value: bool }` |
| `exportPoll` | `GET core/v1/posts/{postId}/poll/export` | File download |

### Payment requests (`PostApi`, `PaymentApi`)

A `PAYMENT` post asks members to pay. `ApiPayment`: `currency`, `title`,
`description`, `amountFormatted`, `amount`, `dueBy`, `mandatory`, `expired`,
`total`, `optionalProducts`, `behalfOfIds`, `respondents`
(`[{ memberId, response, totalPaid, summary }]`), `accountId`, `accountName`,
`provider`, `basePrice`, `payerPaysFee`, `overview`.

| Call | Request | Body / returns |
|------|---------|----------------|
| `getPayment` (*GetPostPayment*) | `GET core/v1/posts/{postId}/payment` | `ApiPayment` |
| `getPaymentCollectionStats` (*GetPaymentCollectionStats*) | `GET core/v1/posts/{postId}/payment/overview` | `{ paidCount, paidAmount, paidAmountFormatted, unpaidCount, unpaidAmount, unpaidAmountFormatted, declinedCount, declinedAmount, declinedAmountFormatted }` |
| `exportPayment` | `GET core/v1/posts/{postId}/payment/export` | File download |
| `addPaymentRespondents` (*AddPaymentRespondents*) | `POST core/v1/posts/{postId}/payment/respondents` | `[{ memberId }]` |
| `removePaymentRespondent` (*RemovePaymentRespondent*) | `DELETE core/v1/posts/{postId}/payment/respondents/{memberId}` | |
| `markPaymentAsPaid` (*MarkPaymentAsPaid*) | `POST core/v1/posts/{postId}/payment/respondents/{memberId}/markPaid` | Admin marks as paid outside the app |
| `declinePayment` (*DeclinePayment*) | `POST core/v1/posts/{postId}/payment/respondents/{memberId}/decline` | |
| `initiatePayment` (*InitiatePayment*) | `POST core/v1/posts/{postId}/payment/respondents/{memberId}/initiate` | `{ products: [{ id, q }] }` → `ApiInitiatedPayment` `{ id, simpleId, currency, countryCode, status, total, payerName, payoutProvider, paymentIntent, ephemeralKey, stripeCustomerId, stripePublishableKey }` |
| `validatePayment` (*ValidatePayment*) | `POST core/v1/posts/{postId}/payment/respondents/{memberId}/validate/{paymentId}` | After Stripe confirms |
| `cancelInitiatedPayment` (*CancelInitiatedPayment*) | `POST core/v1/posts/{postId}/payment/respondents/{memberId}/cancel/{paymentId}` | |

Paying with Stripe (`PaymentApi`), after `initiate`:

| Call | Request | Body |
|------|---------|------|
| `performPaymentWithCard` (*PerformPaymentWithCard*) | `POST core/v1/payments/{paymentId}` | `{ provider: "STRIPE", cardId }` (a saved card) |
| `performPaymentWithMethod` (*PerformPaymentWithMethod*) | `POST core/v1/payments/{paymentId}` | `{ provider: "STRIPE", paymentMethodId, save }` |
| `performPaymentWithIntent` (*PerformPaymentWithIntent*) | `POST core/v1/payments/{paymentId}` | `{ provider: "STRIPE", intentId, save }` |

**Club payments** (membership fees with products and instalments):

| Call | Request | Body / returns |
|------|---------|----------------|
| `getClubPayment` (*GetClubPayment*) | `GET core/v1/posts/{postId}/clubPayment` | `ApiClubPayment` `{ id, title, currency, status, friskusEnabled, clubName, dueDate, amountFormatted, amount, description, clubLogo, familyDiscountRate, familyDiscountMaxPrice, potentialFamilyDiscount, ownerName, ownerEmailAddress, useExactFormat, schedule, billingCycle, schedulePeriods, feeSettings, subscriptions, respondents, receipts }` |
| `calculateClubPaymentFee` (*CalculateClubPaymentFee*) | `POST core/v1/posts/{postId}/clubPayment/calculateFee` | `{ memberProducts: { <memberId>: [productId…] } }` → `{ oneTimeFee, oneTimeToPay, firstInstalmentFee, firstInstalmentToPay, totalInstalmentsFees, totalInstalmentsToPay }` |
| `initiateClubPayment` (*InitiateClubPayment*) | `POST core/v1/posts/{postId}/clubPayment/initiate` | `{ memberProducts, paymentSchedule: "ONE_TIME" \| "INSTALMENT" }` → `ApiInitiatedClubPayment` |
| `validateClubPayment` (*ValidateClubPayment*) | `POST core/v1/posts/{postId}/clubPayment/validate/{paymentId}` | |
| `cancelInitiatedClubPayment` (*CancelInitiatedClubPayment*) | `POST core/v1/posts/{postId}/clubPayment/cancel/{paymentId}` | |
| `selectFriskus` (*SelectFriskus*) | `POST core/v1/posts/{postId}/clubPayment/friskus` | `{ memberProducts, friskusMembershipIds }` → `{ friskusUrl }` (pay via the Norwegian Friskus subsidy scheme) |

**Receipts, cards and subscriptions** (`PaymentApi`):

| Call | Request | Returns |
|------|---------|---------|
| `getPerformedReceipts` (*GetPerformedReceipts*) | `GET core/v1/payments/performed?maxCount=N&maxTimestamp=…` | list of `ApiPaymentReceipt` (what you paid) |
| `getReceivedReceipts` (*GetReceivedReceipts*) | `GET core/v1/payments/received?maxCount=N&maxTimestamp=…` | what your groups received |
| `getGroupReceipts` (*GetGroupReceipts*) | `GET core/v1/payments/group?groupId=…&maxCount=N&maxTimestamp=…` | |
| `getPaymentRecipients` (*GetPaymentRecipients*) | `GET core/v1/groups/{groupId}/payments/recipients?filter=…&order=…` | list of `{ memberId, firstName, lastName, imageUrl, overdueCount, overdueAmounts, overdueAmountFormatted, overdueAmountIsMinimum }` |
| `getPaymentAccounts` (*GetPaymentAccounts*) | `GET core/v1/paymentAccounts` | saved cards `[{ id, last4, brand, expMonth, expYear }]` |
| `markPaymentAccountAsDefault` (*MarkPaymentAccountDefault*) | `PUT core/v1/paymentAccounts/{cardId}/default` | |
| `deletePaymentAccount` (*DeletePaymentAccount*) | `DELETE core/v1/paymentAccounts/{cardId}` | |
| `getSubscriptions` (*GetSubscriptions*) | `GET core/v1/subscriptions` | list of `ApiPaymentSubscription` `{ id, title, currency, type, status, groupName, memberName, noticePeriod, isCancelled, canUnsubscribe, failedCount, lastChargeAt, createdAt, clientSecret, contactPerson, instalments, paymentAccount, items }` |
| `getSubscription` (*GetSubscription*) | `GET core/v1/subscriptions/{subscriptionId}` | `ApiPaymentSubscription` |
| `cancelSubscription` (*CancelSubscription*) | `DELETE core/v1/subscriptions/{subscriptionId}` | |
| `changeSubscriptionPaymentMethod` (*ChangeSubscriptionPaymentMethod*) | `PUT core/v1/subscriptions/{subscriptionId}/paymentSource`, body `{ paymentMethodId, setupId, intentId, cardId }` | |
| `retrySubscriptionPayment` (*RetrySubscriptionPayment*) | `POST core/v1/subscriptions/{subscriptionId}/instalments/{instalmentId}` | Retry a failed instalment |

`ApiPaymentReceipt` holds `id`, `timestamp`, `simpleId`, `payoutProvider`,
`currency`, `countryCode`, `status`, `statementDescription`, `name`, `group`,
payer, recipient and behalf-of names and contact details, `payoutMethod`,
`payoutMethodName`, `paymentSource`, and more.

### Payout accounts (`PayoutApi`)

Where a group's money goes (Stripe Connect). All bodies send `provider: "STRIPE"`.

| Call | Request | Body / returns |
|------|---------|----------------|
| `getPayoutAccounts` (*GetPayoutAccounts*) | `GET core/v1/payoutAccounts` | list of `ApiPayoutAccount` `{ id, status, name, country, currency, payoutSchedule, createdAt, business, payoutAccount, verification, accountLastDigits }` |
| `getPayoutCountries` (*GetPayoutCountries*) | `GET core/v1/payoutAccounts/countries` | list of `PayoutCountry` (country code, currency, field examples, business types) |
| `createPayoutAccount` (*CreatePayoutAccount*) | `POST core/v1/payoutAccounts` | `{ provider, name, country, payoutSchedule, business, fields: { … } }` |
| `createEmbeddedPayoutAccount` (*CreateEmbeddedPayoutAccount*) | `POST core/v1/payoutAccounts/embeddedOnboarding` | `{ country, businessType, name }` |
| `fetchStripeAccountSession` (*FetchStripeAccountSession*) | `POST core/v1/payoutAccounts/{accountId}/accountSession` | `{ account, clientSecret, expiresAt, livemode, embeddedOnboarding }` for Stripe's embedded onboarding |
| `changePayoutAccount` (*ChangePayoutAccount*) | `PUT core/v1/payoutAccounts/{accountId}` | `{ provider, name, … }` |
| `changePayoutSchedule` (*ChangePayoutSchedule*) | `PUT core/v1/payoutAccounts/{accountId}/schedule` | `{ provider, payoutSchedule }` |
| `deletePayoutAccount` (*DeletePayoutAccount*) | `DELETE core/v1/payoutAccounts/{accountId}` | |
| `getPendingFunds` (*GetPendingFunds*) | `GET core/v1/payoutAccounts/{accountId}/balance?provider=STRIPE` | `{ value }` |
| `getStripeUpdateLink` (*GetStripeUpdateLink*) | `GET core/v1/payoutAccounts/{accountId}/stripeUpdateLink` | `{ value: <url> }` |
| `calculateFee` (*CalculateFee*) | `GET core/v1/payoutAccounts/{accountId}/fee?provider=STRIPE&payerPaysFee=…&amount=…` | `{ value }` |
| `calculateFees` (*CalculateFees*) | `POST core/v1/payoutAccounts/{accountId}/fee` | `{ provider, payerPaysFee, amounts: [...] }` → `{ values: [...] }` |
| `exportPayouts` (*ExportPayouts*) | `POST core/v1/payoutAccounts/{accountId}/exportPayouts` | `{ email }`: e-mails a report |

### Fundraising (`CampaignApi`, `frc` server)

Paths are relative to `https://api.spond.com/` and use the fundraising token
(`auth` header, see [Servers and tokens](#servers-and-tokens)).
`Campaign` has `id`, `type`, `status`, `startDate`, `endDate`, `groupId`,
`groupName`, `description`, `currency`, `salesCountGoal`, `salesPriceGoal`,
revenue and profit totals, `participantIds`, `behalfOfIds`, `theme`,
`template`, `promotion`, `prize`, `thanksEmail`, `winner`, and more.

| Call | Request | Body / returns |
|------|---------|----------------|
| `getCampaignsOverview` (*GetCampaignsOverview*) | `GET frc/v1/campaigns/overview?groupId=…` | `{ activeCampaigns, pastCampaigns, upcomingCampaigns, pendingCampaigns }` |
| `getOngoingCampaigns` (*GetOngoingCampaigns*) | `GET frc/v1/campaigns/upcoming?groupId=…`, header `x-spond-frcpromo` | |
| `getPastCampaigns` (*GetPastCampaigns*) | `GET frc/v1/campaigns/past?groupId=…&skip=N` | |
| `getPendingCampaigns` (*GetPendingCampaigns*) | `GET frc/v1/campaigns/pending?groupId=…` | |
| `getCampaign` (*GetCampaignDetails*) | `GET frc/v1/campaign/{campaignId}` | `Campaign` |
| `getCampaignActivities` (*GetCampaignActivities*) | `GET frc/v1/campaigns/activities/{campaignId}` | |
| `getHomeSections` (*GetCampaignHomeSections*) | `GET frc/v1/campaigns/home/{groupId}` | list of `ApiFrcHomeSection` (marketing cards) |
| `getCampaignTemplates` (*GetCampaignTemplates*) | `GET frc/v1/campaigns/types/{groupId}` | list of `CampaignTemplate` (scratch cards, products, raffle, spot-the-ball, super draw) |
| `getCampaignThemes` (*GetCampaignThemes*) | `GET frc/v1/campaigns/templates/{templateId}/themes?startDate=…` | |
| `getProductsByTemplateId` (*GetCampaignProducts*) | `GET frc/v1/campaigns/templates/{templateId}/products` | `CampaignProducts` |
| `getProductsByCampaignId` (*GetCampaignProducts*) | `GET frc/v1/campaign/{campaignId}/products` | `CampaignProducts` |
| `validatePromoCode` (*ValidatePromoCode*) | `GET frc/v1/campaigns/templates/{templateId}/promotion/{code}` | `{ code, description, colour, image, currency }` |
| `getTips` (*GetCampaignTips*) | `GET frc/v1/campaigns/tips/{campaignId}` | list of `{ id, title, subtitle, image, body }` |
| `createCampaign` (*CreateCampaign*) | `POST frc/v1/campaigns` | `{ type, groupId, groupName, description, activity, startDate, salesCountGoal, salesPriceGoal, payoutAccount, prize, participantIds, template: { id }, theme: { id }, promotion: { code } }` |
| `updateCampaign` (*UpdateCampaign*) | `PUT frc/v1/campaign/{campaignId}` | same keys |
| `approveCampaign` (*ApproveCampaign*) | `PUT frc/v1/campaign/{campaignId}/approve` | |
| `declineCampaign` (*DeclineCampaign*) | `PUT frc/v1/campaign/{campaignId}/decline` | |
| `startCampaign` (*StartCampaign*) | `PUT frc/v1/campaign/{campaignId}/start` | |
| `stopCampaign` (*StopCampaign*) | `DELETE frc/v1/campaign/{campaignId}?delete=false` | |
| `deleteCampaign` (*DeleteCampaign*) | `DELETE frc/v1/campaign/{campaignId}?delete=true` | |
| `muteCampaign` (*ToggleCampaignNotifications*) | `PUT frc/v1/campaign/{campaignId}/notifications?enable=true\|false` | |
| `addCampaignParticipants` (*AddCampaignParticipants*) | `POST frc/v1/campaign/{campaignId}/participants/add` | JSON array of member ids |
| `removeCampaignParticipants` (*RemoveCampaignParticipants*) | `POST frc/v1/campaign/{campaignId}/participants/remove` | JSON array of member ids |
| `activateCampaignSeller` (*ActivateCampaignSeller*) | `POST frc/v1/campaign/{campaignId}/activate/{memberId}` | Start selling as (or for) a participant |
| `updateCampaignSellerImage` (*UpdateCampaignSeller*) | `POST frc/v1/sellers/{sellerId}/update` | `{ sellerImage }` |
| `getCampaignThanksHeaders` (*GetCampaignThanksHeaders*) | `GET frc/v1/campaign/{campaignId}/thanksHeaders` | header image URLs |
| `updateCampaignThanksEmail` (*UpdateCampaignThanksEmail*) | `PUT frc/v1/campaign/{campaignId}/thanks` | `{ headerUrl, groupName, title, message, scheduledTime, revenueThreshold, visible }` |
| `disableCampaignThanksEmail` (*DisableCampaignThanksEmail*) | `DELETE frc/v1/campaign/{campaignId}/thanks` | |
| `exportCampaign` | `GET frc/v1/campaign/{campaignId}/export` | File download |
| `exportQRCodePdf` | `GET frc/v1/sellers/salesLinkCode/{code}/qr/pdf?profileId=…` | PDF download |

### File storage (`StorageApi`, `storage` server)

Uploads use a files token for the context (personal, group or coaches' corner;
see [Servers and tokens](#servers-and-tokens)).

1. `POST storage/files/prepareUpload` (*PrepareUpload*), body
   `ApiPrepareUploadRequest` `{ context, type, usage, fileName, folderId }` →
   `{ id, uploadUrl }`.
2. Upload (*UploadFiles*): `POST <uploadUrl>` as `multipart/form-data`, one
   part named `file` per file, with `Content-Disposition: …; filename=…` and
   `Content-Type`. Returns `ApiUploadedFile` `{ id, url, name, mediaType,
   groupId, parentId, length, width, height }`.
   Videos (*UploadVideo*) are sent with `PUT <uploadUrl>` and the raw bytes, then
   processed with `POST storage/video/upload/{uploadId}/process` (*StartVideoProcessing*),
   body `{}`. A post's `media[].uploadStatus` tracks the processing.
3. `GET storage/upload/group/{fileId}` (*GetGroupFileLink*) → `{ url }`: a
   download link for a group file.

`downloadFile` is a plain `GET` of the file URL.

### Messages: extra chat calls (`ChatApi`)

Paths are relative to the chat `url`, with the chat `auth` header (see
[Messages](#messages-chatapi)).

| Call | Request | Body / returns |
|------|---------|----------------|
| `deleteMessage` (*DeleteChatMessage*) | `DELETE chats/{chatId}/{msgNum}` | |
| `changeThreadName` (*ChangeChatThreadName*) | `PUT chats/{chatId}/name` | `{ value }` |
| `addParticipants` (*AddChatParticipants*) | `POST chats/{chatId}/participants` | JSON array of profile ids |
| `removeParticipant` (*RemoveChatParticipant*) | `DELETE chats/{chatId}/participants/{profileId}` | Also used to leave a thread |
| `muteThread` / `unmuteThread` (*MuteChatThread* / *UnmuteChatThread*) | `PUT` / `DELETE chats/{chatId}/mute` | |
| `archiveThreads` (*ArchiveChatThreads*) | `POST chats/archive` | JSON array of thread ids |
| `unarchiveThreads` (*UnarchiveChatThreads*) | `POST chats/unarchive` | JSON array of thread ids |
| `markThreadsAsUnread` (*MarkChatThreadsAsUnread*) | `PUT chats/bulkUnread` | JSON array of thread ids |
| `reportThread` (*ReportChatThread*) | `POST chats/{chatId}/report` | Body: a set of reason strings |
| `reviveDirectThread` (*ReviveDirectChatThread*) | `POST messages` | `{ chatId, type: "ADD" }`: re-adds you to a direct thread you left |
| `sendSeparately` (*SendBulkChatMessages*) | `POST messages/bulk` | Like a first message (`community`, `recipients`, `text`, `type`, …) but sends one direct message to each recipient |
| `getAgeEligibilityOfGroup` (*GetChatAgeEligibilityOfGroup*) | `GET chats/groupRecipients/{groupId}` | `{ standard, underage, invalid_underage }`: who may be messaged, given `chatAgeLimit` |
| `canChatWith` (*CanChatWith*) | `GET chats/groupRecipients/{groupId}/profile/{profileId}` | |
| `getValidGroupsForProfile` (*GetValidChatGroupsForProfile*) | `GET chats/groupsForRecipient/{profileId}` | Groups through which you may message this person |

Message bodies can also carry `file`, `images`, `spond { id }`,
`post { id }` and `campaign { id }` (`ChatApi.toJsonObject`). Received
messages may have the extra types listed in `ApiChatMessage`: `promo`,
`internalPromo`, `members` / `total` (participants added or removed) and
`newName` (thread renamed).

## Minimal flow

```
POST /core/v1/auth2/login                  -> accessToken.token
     (/core/v1/auth2/2fa/verify if a 2FA challenge comes back)
GET  /core/v1/groups?includeMembers=true   -> pick a group id
GET  /core/v1/posts?type=PLAIN&groupId={id}&max=30&includeComments=false
GET  …&maxTimestamp={oldest.timestamp}&prevId={oldest.id}   (next page)
```

## Example client: `spondcli/`

See the [README](../README.md) for the code layout.

```
cd spondcli
go run ./cmd/spondcli                                            # interactive UI (Bubble Tea)
go run ./cmd/spondcli members -group "Team name" -o members.csv  # print + export members
go run ./cmd/spondcli members -group "Team name" -subgroup "U12" -o u12.csv
go run ./cmd/spondcli search -group "Team name" dugnad kiosk     # search posts and comments
go run ./cmd/spondcli search -group "Team name" -subgroup "U12" -refresh kamp
go run ./cmd/spondcli logout                                     # forget the cached login
```

Credentials come from `SPOND_EMAIL` / `SPOND_PASSWORD`, or are prompted for.
After a login the access token is cached in the user config directory
(`%AppData%`, `~/Library/Application Support` or `~/.config`) as
`spondcli/session.json` (mode 0600) and reused until 5 minutes before it
expires. A 401 deletes the cache and asks for a new login.

Search keeps a SQLite cache (`modernc.org/sqlite`, pure Go) in the user cache
directory (`%LocalAppData%`, `~/Library/Caches` or `~/.cache`) as
`spondcli/posts.db`. Because the API offers no change detection (see
[Caching and change detection](#caching-and-change-detection)), a search
re-downloads the group feed plus every subgroup feed, with comments, when the
cache is older than 15 minutes or `-refresh` is given. All downloads must
succeed before anything is written. If the sync fails, the old cache is
searched with a warning. Matching uses an FTS5 index over title, body and
comments: case and accent insensitive, every word must match, and each word
is a prefix.

While the interactive UI runs, it keeps itself up to date like the app:
every minute it reads the activity stream from its cursor
(`core/v1/activities?order=asc&since=…`) and re-fetches only the
arrangements and posts the new activities name. Changed ones are updated in
place (also in an open modal), new ones are added to Home, the group's
arrangements and the posts feed when they fall in what is loaded, and deleted
ones are removed. Changed posts are also written to the search cache. New
activities reload Alerts, and group or membership changes reload the groups.
Each minute it also reads the newest page of chat threads and adds new
messages to an open conversation (following them only when scrolled to the
bottom). Poll errors are not shown; the next minute tries again.

The interactive UI has four tabs, as in the app: **Home**, **Alerts**,
**Groups** and **Messages**. `1`–`4` or a click on the tab bar switches tab;
each tab keeps its place. Alerts and Messages show unread counts.

Interactive keys:

- Home: arrangements from all your groups, upcoming first. Same keys as a
  group's arrangements (below); `r` reloads.
- Alerts: the app's notifications, unread ones marked `●`. `enter` or a
  click opens the arrangement (where you can answer) or post it points to;
  `r` reloads. Alerts are not marked as read.
- Messages: conversations, newest first; `enter` opens one, oldest message
  at the top. A coloured banner names the conversation (💬 group,
  👤 direct message, 📣 club message) and who is in it. Messages show as
  bubbles, yours on the right; `↑`/`↓` (or a click) select one, marked with
  `▶` and a thick border, and reaching the top loads older messages.
  Reactions show under each message, yours highlighted, and who reacted
  under the selected one. `e` opens the reaction picker (`←`/`→` and
  `enter`, or `1`–`9`, `0`); `+` toggles 👍; picking your current reaction
  removes it. `r` reloads; `esc` goes back. `i` or `enter` moves to the
  message box:
  `enter` sends, `alt+enter` (or `ctrl+j`) adds a line break, `esc` leaves
  the box. In a group conversation `@` tags someone, as in the app: type to
  filter the participants (a guardian also matches on the child's name),
  `↑`/`↓` choose, `tab` or `enter` inserts `@Full Name`, `esc` closes the
  list. Backspace right after a tag turns it back into `@`. Tags are sent as
  mentions; received tags show as `@Name`. `n` starts a new conversation: choose the group, type to filter
  people, `tab` picks several, `enter` opens the conversation (an existing
  one with the same people is reused). Threads are not marked as read.
- Groups: `enter` opens the group menu (Posts / Arrangements / Members, plus
  Subgroup when the group has any); `p`, `a`, `m` jump straight to a section
  for the whole group; `/` filters; `q` quits.
- Group menu: `s` picks a subgroup (or "Whole group"). Posts, arrangements and
  members then show only that subgroup; the breadcrumb shows `Group › Subgroup`.
- Posts: `↑`/`↓` select a post (the next page loads at the bottom), `enter`
  or a click opens it with all comments. `/` searches the group's (or
  subgroup's) posts and comments; results show the matching comments with the
  matched words highlighted. In an opened post, `n`/`tab` and `N`/`shift+tab`
  jump between matches. `esc` clears the search.
- `g`/`home` and `G`/`end` jump to the top and bottom in lists and modals.
- With a cached login, or `SPOND_EMAIL`/`SPOND_PASSWORD` set, the login form
  is skipped.
- Arrangements: `↑`/`↓` select a card, `enter` or a mouse click opens the
  detail modal, `tab` switches between Upcoming and Past.
- Arrangement modal: every field with a readable label, plus **Your answer**.
  `y` attend, `n` can't attend (optional message), `tab` switches person when you
  answer for several (e.g. children), `r` refreshes, `↑`/`↓` scroll, `esc` closes.
- Members: `↑`/`↓` select, `enter` or a click opens the member modal with all
  data; `e` exports `<group-name>-members.csv`; `i` opens the invite form.
- Main screen: when you can manage members somewhere, a **Pending join
  requests** pane lists every request; `tab` switches to it, `enter` or a click
  opens a request: `a` approve (tick subgroups, `enter`), `x` reject.
- Member modal (admin actions only appear when you have the rights): `e` edit
  name, email, phone and custom fields, `s` subgroups, `r` admin roles, `m` make member / remove member status, `g` add
  guardian, `d` remove from group (asks for confirmation).
- Invite form (`i`, also in the group menu): choose the group and Member/Admin,
  fill in the person, subgroups, admin roles or an optional guardian, then
  submit.
- Loading shows a progress bar; member loading shows real progress.
- `esc` goes back to the group menu.
