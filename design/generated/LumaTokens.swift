// Generated from design/tokens.json — do not edit.

public struct LumaColor: Sendable {
    public let light: UInt32
    public let dark: UInt32
}

public struct LumaTypeStyle: Sendable {
    public let size: Double
    public let lineHeight: Double
    public let weight: Int
    public let tracking: Double
    public let display: Bool
    public let tabular: Bool
}

public enum LumaTokens {
    public static let displayFont = "Instrument Sans"

    public enum Color {
        public static let ground = LumaColor(light: 0xF1F3F4, dark: 0x0B0F12)
        public static let surface = LumaColor(light: 0xFFFFFF, dark: 0x11161A)
        public static let surfaceRaised = LumaColor(light: 0xFFFFFF, dark: 0x192025)
        public static let surfaceSunken = LumaColor(light: 0xE9EDEF, dark: 0x0B0F12)
        public static let hairline = LumaColor(light: 0xDDE2E5, dark: 0x232B31)
        public static let text = LumaColor(light: 0x0E1418, dark: 0xECF0F2)
        public static let textSecondary = LumaColor(light: 0x47525A, dark: 0xA7B1B8)
        public static let textTertiary = LumaColor(light: 0x5F6A72, dark: 0x86919A)
        public static let bubbleOut = LumaColor(light: 0x141A1E, dark: 0xE8EDF0)
        public static let onBubbleOut = LumaColor(light: 0xF2F5F6, dark: 0x0B0F12)
        public static let bubbleOutMeta = LumaColor(light: 0xA3ADB3, dark: 0x4E5961)
        public static let bubbleIn = LumaColor(light: 0xE9EDEF, dark: 0x1C2328)
        public static let onBubbleIn = LumaColor(light: 0x0E1418, dark: 0xECF0F2)
        public static let bubbleInMeta = LumaColor(light: 0x56616A, dark: 0x8E99A1)
        public static let primary = LumaColor(light: 0x141A1E, dark: 0xE8EDF0)
        public static let onPrimary = LumaColor(light: 0xF2F5F6, dark: 0x0B0F12)
        public static let lamp = LumaColor(light: 0xF2AE2E, dark: 0xF5BC4C)
        public static let onLamp = LumaColor(light: 0x1A1405, dark: 0x1A1405)
        public static let selection = LumaColor(light: 0xE3E8EB, dark: 0x1E262B)
        public static let danger = LumaColor(light: 0xB8322A, dark: 0xFF8277)
        public static let onDanger = LumaColor(light: 0xFFFFFF, dark: 0x1A0705)
        public static let focusRing = LumaColor(light: 0x0E1418, dark: 0xECF0F2)
        public static let scrim = LumaColor(light: 0x0E1418, dark: 0x000000)
    }

    public enum Space {
        public static let s0: Double = 0
        public static let s1: Double = 2
        public static let s2: Double = 4
        public static let s3: Double = 8
        public static let s4: Double = 12
        public static let s5: Double = 16
        public static let s6: Double = 20
        public static let s7: Double = 24
        public static let s8: Double = 32
        public static let s9: Double = 40
        public static let s10: Double = 48
        public static let s11: Double = 64
    }

    public enum Size {
        public static let avatarS: Double = 28
        public static let avatarM: Double = 44
        public static let avatarL: Double = 56
        public static let avatarXL: Double = 96
        public static let hitMin: Double = 44
        public static let row: Double = 72
        public static let composerMin: Double = 44
        public static let badge: Double = 20
        public static let presenceDot: Double = 10
        public static let lumaLine: Double = 2
        public static let sidebar: Double = 264
        public static let listPane: Double = 340
        public static let bubbleMax: Double = 560
    }

    public enum Radius {
        public static let xs: Double = 6
        public static let s: Double = 10
        public static let m: Double = 14
        public static let bubble: Double = 20
        public static let bubbleJoined: Double = 6
        public static let sheet: Double = 24
        public static let pill: Double = 999
    }

    public enum Typography {
        public static let largeTitle = LumaTypeStyle(size: 32, lineHeight: 38, weight: 600, tracking: -0.6, display: true, tabular: false)
        public static let title = LumaTypeStyle(size: 22, lineHeight: 28, weight: 600, tracking: -0.3, display: true, tabular: false)
        public static let headline = LumaTypeStyle(size: 16, lineHeight: 21, weight: 600, tracking: -0.1, display: false, tabular: false)
        public static let body = LumaTypeStyle(size: 16, lineHeight: 22, weight: 400, tracking: 0, display: false, tabular: false)
        public static let callout = LumaTypeStyle(size: 15, lineHeight: 20, weight: 400, tracking: 0, display: false, tabular: false)
        public static let subhead = LumaTypeStyle(size: 14, lineHeight: 19, weight: 400, tracking: 0, display: false, tabular: false)
        public static let footnote = LumaTypeStyle(size: 13, lineHeight: 18, weight: 400, tracking: 0, display: false, tabular: false)
        public static let caption = LumaTypeStyle(size: 12, lineHeight: 16, weight: 500, tracking: 0.1, display: false, tabular: true)
    }

    public enum Duration {
        public static let instant: Double = 90
        public static let quick: Double = 160
        public static let standard: Double = 240
        public static let emphasized: Double = 380
        public static let lumaPulse: Double = 1600
    }

    public enum Spring {
        public static let send = (response: 0.35, damping: 0.82)
        public static let sheet = (response: 0.45, damping: 0.9)
    }

    public enum Elevation {
        public static let overlayY: Double = 12
        public static let overlayBlur: Double = 32
        public static let overlayAlphaLight: Double = 0.14
        public static let overlayAlphaDark: Double = 0.55
    }
}
