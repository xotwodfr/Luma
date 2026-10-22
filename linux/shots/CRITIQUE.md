# Linux client: visual verification against the M1 preview

Shots are produced by `linux/scripts/shoot.sh`: a throwaway server is seeded with the preview's conversations,
Mira and Elif are online through real headless clients, and Qt renders offscreen with the software backend.
Compare them with `design/preview/shots/linux-*.png`.

## Round 1 → fixes

| # | What the screenshot showed | Cause | Fix |
|---|---|---|---|
| 1 | The primary button label was black on black. | QML parses `onPrimary` (on + Uppercase) as a signal handler, so the token never existed. | The generator emits `textOn*` names for QML, with a test that forbids `on[A-Z]` property names. |
| 2 | An opened chat showed only its last message. | The conversation list caches `last_message`; the "is the cache empty?" check then skipped fetching history. | Opening a conversation always fetches the latest page (idempotent upserts). |
| 3 | The composer was 58 px tall with the placeholder top-aligned. | Height was derived from ScrollView content plus padding. | Height comes from `TextArea.implicitHeight` (42 px for one line), vertically centred. |
| 4 | The presence halo was heavy against the light list. | 1.5 px ink ring. | 1.25 px. |
| 5 | Dark sign-in: the text fields were invisible. | Fields (`surfaceSunken`) sat on `ground`; in dark mode they are the same value. | The sign-in page sits on `surface`, the same pairing every other input uses. |
| 6 | Button labels were left-aligned. | The Row contentItem wasn't centred. | Wrapped and centred. |
| 7 | No way to sign out at two-pane width. | The account button lived only in the rail (≥ 1100 px). | `AccountButton` component, shown in the list header below 1100 px. |
| 8 | The sign-in footer joined host and privacy note with a middle dot. | Templated meta-string pattern. | Two plain lines. |

## Matches the preview
- Luminance inversion: ink bubbles in light, lit bubbles in dark.
- The lamp only on unread badges and the selection marker.
- Halo presence from real sockets.
- Runs with tightened inner corners, and date separators.
- The receipt states read, sent (outline) and failed (verified at 8× zoom).
- Display face on titles; the platform UI font elsewhere.

## Known differences (by design or scope)
- No group conversations or file bubbles yet (Milestone 4).
- The chat header has no search or more actions until those features exist.
- Last-seen times are not shown (presence `last_seen` isn't stored client-side yet).
- Reduced motion: Qt exposes no portable reduced-motion setting on Linux, so the luma line still breathes. Tracked for M7.
