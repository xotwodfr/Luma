# Luma — Design Language

The source of truth for values is `design/tokens.json`. Generated platform files live in
`design/generated/`. This document records the *rules*: how the tokens are used, and how each platform adapts them.

## 1. Identity: luminance

Luma is named after the luminance channel, and the identity is built on that one idea.
The interface is almost achromatic: cool mineral greys with a faint blue-green cast. Hierarchy comes from
*lightness*, not hue.

- **Your words are the light.** Messages you send use `bubbleOut`, the luminance inverse of the ground. They
  are deep ink in light mode and lit near-white in dark mode. Received messages use `bubbleIn`, a quiet tonal step.
  Primary buttons use the same inversion (`primary` / `onPrimary`), so "your action" always looks like "your voice".
- **The lamp.** `lamp` (amber) is the only chromatic color. It works like an indicator lamp on an instrument and means
  "needs your attention". It may appear only on the unread badge and the selected-conversation marker.
  It is never a text color, never a large fill, never decorative.
- **Presence is luminance too.** Someone who is online has a halo: a 1.5px `primary` ring around the avatar, separated
  by a 2px `surface` gap. Amber isn't used for presence because other apps use it to mean "away", and because it
  would compete with unread badges on the same row.
- **The luma line.** A 2px line (`size.lumaLine`) under the conversation header shows connection state.
  Connected: invisible. Connecting or reconnecting: it breathes (opacity 0.25 → 1, `duration.lumaPulse`, standard curve).
  Offline: steady `textTertiary` with a text status. This is Luma's one signature motion.
- No gradients, glass, glow, decorative cards or emoji-as-icons. Separation comes from hairlines and tonal steps.
  The only shadow is `elevation.overlay`, used for floating surfaces (menus, popovers, sheets).

## 2. Typography

| Role | Token | Face | Use |
|---|---|---|---|
| Display | `largeTitle`, `title` | Instrument Sans (bundled, OFL) | Screen titles, wordmark, empty-state headings, profile names |
| UI | `headline` … `caption` | Platform system font | Everything else |

- UI faces: SF Pro (iOS, macOS), Roboto Flex (Android), Segoe UI Variable (Windows), and the desktop font on Linux (Noto Sans on KDE, Cantarell on GNOME, via the Qt platform theme).
  Each platform's own face keeps text native. The display face carries the identity.
- `caption` uses tabular figures (timestamps, counts, seq-ordered lists) so numbers don't jitter. No monospace in the UI.
- Sentence case everywhere. No all-caps labels and no letter-spaced eyebrows.
- Message text is `body`. Bubble width is capped at `size.bubbleMax` (≈ 65 characters per line).
- Text scaling: everything scales with the platform's text size setting up to 200%. The layout reflows instead of truncating message text.

## 3. Color roles

| Role | Tokens |
|---|---|
| Canvas | `ground` (window/background), `surface` (lists, chat pane), `surfaceRaised` (menus, sheets), `surfaceSunken` (inputs) |
| Lines | `hairline`, 1px (1 device pixel on HiDPI) |
| Text | `text`, `textSecondary` (previews, subtitles), `textTertiary` (timestamps, hints) |
| Messages | `bubbleOut`/`onBubbleOut`/`bubbleOutMeta`, `bubbleIn`/`onBubbleIn`/`bubbleInMeta` |
| Action | `primary`/`onPrimary`, `danger`/`onDanger` |
| Signal | `lamp`/`onLamp` |
| State | `selection` (selected row), `focusRing`, `scrim` (behind modals, at 40% light / 60% dark) |

- Every text pair passes WCAG AA, enforced by `npm run contrast` in CI.
- Dark mode is designed in its own right. The ground is `#0B0F12`, never `#000`. Tonal steps grow with elevation:
  ground → surface → surfaceRaised.
- High contrast or increased contrast: hairlines become `textTertiary`, and `bubbleIn` gets a hairline border.
- Android dynamic color is off; identity wins. Windows accent color is not used. Luma's own inversion handles emphasis.

## 4. Space, size, radius

- Spacing follows a 4-pt scale (`space.1`…`space.11` = 2…64). Screen gutters are `space.5` (16) on phones and `space.7` (24) on desktop.
- Hit targets are at least `size.hitMin` (44pt / 48dp on Android, which overrides the token).
- Avatars: `avatarS` 28 (group sender), `avatarM` 44 (rows), `avatarL` 56 (header on mobile), `avatarXL` 96 (profile).
- Radii have fixed tiers, never in between:
  - `xs` 6: badges' inner shapes, chips.
  - `s` 10: inputs, buttons.
  - `m` 14: popovers.
  - `bubble` 20 and `bubbleJoined` 6: message grouping.
  - `sheet` 24.
  - `pill`: the composer and badges.
- **Bubble grouping:** consecutive messages from the same sender within 3 minutes form a group. Corners facing a
  neighbour in the group use `bubbleJoined`; the outer corners stay `bubble`. There are no tails.
  Groups are separated by `space.4` (12), and bubbles inside a group by `space.1` (2).

## 5. Motion

Every animation explains a state change. If it doesn't, it doesn't ship.

| Change | Motion |
|---|---|
| Message sent | The bubble rises from the composer into place: `spring.send`, plus an opacity fade-in from 0.6. |
| Message received | Fades in over `duration.quick`, with no slide. The list doesn't jump if you've scrolled up; a "new messages" pill appears instead. |
| Receipt changes | The circle fills over `duration.standard`, with no bounce. |
| Navigation push/pop | Platform default (iOS push, Android predictive back, WinUI connected animation, Qt StackView with standard curve). |
| Sheet or popover | `spring.sheet` on Apple, the platform default elsewhere. |
| Typing indicator | Three dots in `textTertiary`. Each dot's opacity cycles with a 160ms offset; nothing moves vertically. |
| Connection | The luma line (§1). |

- Durations: `instant` 90 (hover, press), `quick` 160, `standard` 240, `emphasized` 380.
- **Reduced motion:** springs and slides become 90ms crossfades, the luma line stops breathing and stays steady, and typing dots stay static.

## 6. Iconography

Each platform uses its native symbol set, at a 1.5px stroke equivalent. The semantic names are shared:

| Semantic | SF Symbols | Material Symbols Rounded | Fluent UI System Icons | Lucide (Linux) |
|---|---|---|---|---|
| send | `arrow.up` | `arrow_upward` | `arrow_up` | `arrow-up` |
| attach | `paperclip` | `attach_file` | `attach` | `paperclip` |
| newChat | `square.and.pencil` | `edit_square` | `compose` | `square-pen` |
| search | `magnifyingglass` | `search` | `search` | `search` |
| settings | `gearshape` | `settings` | `settings` | `settings` |
| back | `chevron.left` | `arrow_back` | `arrow_left` | `chevron-left` |
| more | `ellipsis` | `more_vert` | `more_horizontal` | `ellipsis` |
| pending | `circle.dashed` | `radio_button_unchecked` | `circle` (regular) | `circle-dashed` |
| delivered | `circle.lefthalf.filled` | `contrast` | `circle_half_fill` | `circle-half` (custom glyph) |
| read | `circle.fill` | `circle` (filled) | `circle` (filled) | `circle` (filled) |
| failed | `exclamationmark.circle` | `error` | `error_circle` | `circle-alert` |
| block | `hand.raised` | `block` | `prohibited` | `ban` |
| mute | `bell.slash` | `notifications_off` | `alert_off` | `bell-off` |
| archive | `archivebox` | `archive` | `archive` | `archive` |
| group | `person.2` | `group` | `people` | `users` |

Receipts follow the luminance idea: pending is an outline circle, delivered is half lit, read is fully lit.
They are drawn in `bubbleOutMeta`, including the failed glyph, because `danger` on an ink or lit bubble fails contrast.
The red belongs to the "Couldn't send. Tap to retry." note under the bubble, which sits on `surface`.
The app icon is the one custom glyph: a lit disc partly eclipsed by an ink disc, on `ground`.

## 7. Components

**Conversation row**
- Anatomy: a 44 avatar with an optional presence halo, then the name (`headline`),
  the time (`caption`, tabular, right-aligned), the preview (`subhead`, `textSecondary`, one line), and the unread badge (lamp).
- States: default, hover (`selection` at 60%, desktop only), selected (`selection` plus a 3px lamp marker on the leading edge),
  muted (bell-slash, and the badge becomes `textTertiary` outline), focused (2px `focusRing`, inset 2).
- Screen reader: "Name, 3 unread, last message: …, 14:02".

**Message bubble**
- Grouping as in §4. Metadata (time and receipt) sits inside the last bubble of the group, aligned to the trailing edge, in `caption`.
- Long-press (touch), right-click or the menu key (desktop) opens the message actions.
- Screen reader: "You, 14:02, read: …".

**Composer**
- A pill-shaped `surfaceSunken` field holding attach, the multiline text field (up to 6 lines, then it scrolls) and send.
- Send is a 32 `primary` circle and is hidden until there is text or an attachment.
- Enter sends on desktop and Shift+Enter adds a new line; on mobile, Return adds a new line.

**Status line**
- The luma line plus a one-line label under the header while not connected: "Connecting…", "Reconnecting in 4 s", "Offline — messages will send when you're back".

**Unread badge**
- A pill with min-width `size.badge`, `lamp` fill, `onLamp` text in `caption`. Counts above 99 show as "99+".

**Empty states**
- A display-face heading, one sentence, and one primary action. No illustration.
  - List: "No conversations yet" / "Start one with someone's username." / [New conversation].
  - Chat: "Say hello to {name}".

**Loading**
- Skeleton rows in `surfaceSunken` with no shimmer; the luma line breathes instead.
- Cached content always renders first.

**Errors**
- Inline, specific, with a fix. For example: "Couldn't send. Tap to retry." on a failed message (`danger` receipt).

## 8. Platform adaptations

| | Structure | Specifics | Guideline |
|---|---|---|---|
| iOS | `NavigationStack`: list → chat. Large title "Chats" in the display face. | Swipe actions (mute, archive). Sheets for new chat and profile. Light haptic on send. Dynamic Type. | [Apple HIG](https://developer.apple.com/design/human-interface-guidelines) |
| macOS | `NavigationSplitView`: sidebar (Chats, Archived, Settings) / list / chat. Unified toolbar. | ⌘N new chat, ⌘F search, ⌘1–9 jump to chat, ⌘, settings. Menu bar commands. Multiple windows. Vibrancy only on the sidebar (system material). | [Apple HIG — macOS](https://developer.apple.com/design/human-interface-guidelines/designing-for-macos) |
| Android | Material 3 structure re-skinned with Luma tokens: top app bar, FAB "New chat", list → chat. Two-pane `ListDetailPaneScaffold` at expanded width. | Predictive back. Edge-to-edge. 48dp targets. Dynamic color off. | [Material 3](https://m3.material.io) |
| Windows | `NavigationView` (left compact) plus list-detail. Mica on the window backdrop only. | Fluent focus visuals. Ctrl+N, Ctrl+F. Toast notifications. Title bar extended into the app. | [Fluent 2](https://fluent2.microsoft.design) · [Windows app design](https://learn.microsoft.com/windows/apps/design/) |
| Linux | Qt Quick, custom Luma style: three panes (narrow rail / list / chat) at ≥ 1100px, two panes below that. | Follows the system dark preference (portal `color-scheme`). Ctrl+N, Ctrl+K quick switcher. Tray icon. freedesktop notifications. Client-side decorations only where the desktop lacks them. | [KDE HIG](https://develop.kde.org/hig/) · [GNOME HIG](https://developer.gnome.org/hig/) |

## 9. Accessibility checklist
- [ ] All text pairs pass AA (`npm run contrast`).
- [ ] Every control has an accessible name. Images have alt text, either generated ("Photo from Alice") or a caption.
- [ ] Full keyboard operation on desktop, with a visible `focusRing` (2px, offset 2).
- [ ] Text scaling to 200% without clipping message text.
- [ ] Reduced motion and reduced transparency respected.
- [ ] State is never conveyed by color alone. The presence halo and the lamp always have a text equivalent (header "Online", row label).
- [ ] Hit targets meet the minimum size.

## 10. References, and what each one is used for
- **Apple HIG, Material 3, Fluent 2, KDE/GNOME HIG:** structure and platform behaviour (§8). They aren't used for the visual identity.
- **Things 3:** calm hierarchy, with emphasis carried by space and weight rather than color.
- **Linear:** typographic restraint, a single strong neutral system, and fast, small motion.
- **Teenage Engineering product design:** the indicator-lamp idea. One small, precise signal color on a neutral instrument.
- **Analog photography and the luminance channel:** the inversion concept (your messages are the light).
