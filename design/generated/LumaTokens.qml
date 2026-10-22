// Generated from design/tokens.json — do not edit.
pragma Singleton
import QtQuick

QtObject {
    property bool dark: false
    readonly property string displayFont: "Instrument Sans"
    readonly property string uiFont: "Inter"

    readonly property color ground: dark ? "#0B0F12" : "#F1F3F4"
    readonly property color surface: dark ? "#11161A" : "#FFFFFF"
    readonly property color surfaceRaised: dark ? "#192025" : "#FFFFFF"
    readonly property color surfaceSunken: dark ? "#0B0F12" : "#E9EDEF"
    readonly property color hairline: dark ? "#232B31" : "#DDE2E5"
    readonly property color text: dark ? "#ECF0F2" : "#0E1418"
    readonly property color textSecondary: dark ? "#A7B1B8" : "#47525A"
    readonly property color textTertiary: dark ? "#86919A" : "#5F6A72"
    readonly property color bubbleOut: dark ? "#E8EDF0" : "#141A1E"
    readonly property color onBubbleOut: dark ? "#0B0F12" : "#F2F5F6"
    readonly property color bubbleOutMeta: dark ? "#4E5961" : "#A3ADB3"
    readonly property color bubbleIn: dark ? "#1C2328" : "#E9EDEF"
    readonly property color onBubbleIn: dark ? "#ECF0F2" : "#0E1418"
    readonly property color bubbleInMeta: dark ? "#8E99A1" : "#56616A"
    readonly property color primary: dark ? "#E8EDF0" : "#141A1E"
    readonly property color onPrimary: dark ? "#0B0F12" : "#F2F5F6"
    readonly property color lamp: dark ? "#F5BC4C" : "#F2AE2E"
    readonly property color onLamp: dark ? "#1A1405" : "#1A1405"
    readonly property color selection: dark ? "#1E262B" : "#E3E8EB"
    readonly property color danger: dark ? "#FF8277" : "#B8322A"
    readonly property color onDanger: dark ? "#1A0705" : "#FFFFFF"
    readonly property color focusRing: dark ? "#ECF0F2" : "#0E1418"
    readonly property color scrim: dark ? "#000000" : "#0E1418"

    readonly property QtObject space: QtObject {
        readonly property real s0: 0
        readonly property real s1: 2
        readonly property real s2: 4
        readonly property real s3: 8
        readonly property real s4: 12
        readonly property real s5: 16
        readonly property real s6: 20
        readonly property real s7: 24
        readonly property real s8: 32
        readonly property real s9: 40
        readonly property real s10: 48
        readonly property real s11: 64
    }
    readonly property QtObject size: QtObject {
        readonly property real avatarS: 28
        readonly property real avatarM: 44
        readonly property real avatarL: 56
        readonly property real avatarXL: 96
        readonly property real hitMin: 44
        readonly property real row: 72
        readonly property real composerMin: 44
        readonly property real badge: 20
        readonly property real presenceDot: 10
        readonly property real lumaLine: 2
        readonly property real sidebar: 264
        readonly property real listPane: 340
        readonly property real bubbleMax: 560
    }
    readonly property QtObject radius: QtObject {
        readonly property real xs: 6
        readonly property real s: 10
        readonly property real m: 14
        readonly property real bubble: 20
        readonly property real bubbleJoined: 6
        readonly property real sheet: 24
        readonly property real pill: 999
    }
    readonly property QtObject duration: QtObject {
        readonly property real instant: 90
        readonly property real quick: 160
        readonly property real standard: 240
        readonly property real emphasized: 380
        readonly property real lumaPulse: 1600
    }
    readonly property QtObject type: QtObject {
        readonly property var largeTitle: ({ size: 32, lineHeight: 38, weight: 600, tracking: -0.6, display: true, tabular: false })
        readonly property var title: ({ size: 22, lineHeight: 28, weight: 600, tracking: -0.3, display: true, tabular: false })
        readonly property var headline: ({ size: 16, lineHeight: 21, weight: 600, tracking: -0.1, display: false, tabular: false })
        readonly property var body: ({ size: 16, lineHeight: 22, weight: 400, tracking: 0, display: false, tabular: false })
        readonly property var callout: ({ size: 15, lineHeight: 20, weight: 400, tracking: 0, display: false, tabular: false })
        readonly property var subhead: ({ size: 14, lineHeight: 19, weight: 400, tracking: 0, display: false, tabular: false })
        readonly property var footnote: ({ size: 13, lineHeight: 18, weight: 400, tracking: 0, display: false, tabular: false })
        readonly property var caption: ({ size: 12, lineHeight: 16, weight: 500, tracking: 0.1, display: false, tabular: true })
    }
}
