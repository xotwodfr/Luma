# Preview critique log

Each round: screenshots taken by `node tools/shoot.mjs`, inspected, weaknesses and fixes recorded here.

## Round 1 (2026-10-22)

| # | Weakness | Fix |
|---|---|---|
| 1 | Presence (lamp dot) and the unread badge (lamp) sit next to each other on the same row: two meanings, one color. Amber also reads as "away" in Slack and Teams. | Presence becomes a luminance halo: a 1.5px `primary` ring with a `surface` gap around the avatar. The lamp now means "needs your attention" only: unread badges and the selection marker. DESIGN.md §1 and §7 updated. |
| 2 | Group avatars overlap two-letter initials, so "JW" is clipped. | Paired avatars use single initials at a larger offset. |
| 3 | In group chats the sender's avatar sits beside the name instead of the last bubble. | Each run is a row with a 28px avatar column aligned to the bottom, and the name and bubbles stacked beside it. |
| 4 | Failed send: the red time and glyph sit on the ink bubble at ≈2.6:1, failing AA. The token gate can't see component-level pairings. | Bubble metadata keeps `bubbleOutMeta`; the failure glyph is drawn in that color. Red appears only in the note on `surface` (`danger` on `surface` is gated). DESIGN.md §6 updated. |
| 5 | Loading state: the header collapses (`flex: 0` gives a zero basis) and the skeleton rows overlap it. | `flex: none`. |
| 6 | Five states in a four-column grid leave one alone on a row. | Added the real "new conversation" state ("Say hello to Noor"). Six states in a 3 × 2 grid. |
| 7 | Phone chats open with a large empty band above "Today". | Added yesterday's messages, so the hero reads as a real thread. |
| 8 | macOS sidebar has a stray "Luma" section label that isn't a macOS convention. | Removed. |

Kept on purpose: muted conversations show a quiet outlined badge, so the count stays visible without the lamp, and the Android FAB overlaps the last row as Material does.
