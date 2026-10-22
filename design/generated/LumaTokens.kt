// Generated from design/tokens.json — do not edit.
package app.luma.design

object LumaTokens {
    const val DISPLAY_FONT = "Instrument Sans"

    data class TypeStyle(val size: Float, val lineHeight: Float, val weight: Int, val tracking: Float, val display: Boolean, val tabular: Boolean)
    data class SpringSpec(val response: Float, val damping: Float)

    object LightColors {
        const val ground: Long = 0xFFF1F3F4L
        const val surface: Long = 0xFFFFFFFFL
        const val surfaceRaised: Long = 0xFFFFFFFFL
        const val surfaceSunken: Long = 0xFFE9EDEFL
        const val hairline: Long = 0xFFDDE2E5L
        const val text: Long = 0xFF0E1418L
        const val textSecondary: Long = 0xFF47525AL
        const val textTertiary: Long = 0xFF5F6A72L
        const val bubbleOut: Long = 0xFF141A1EL
        const val onBubbleOut: Long = 0xFFF2F5F6L
        const val bubbleOutMeta: Long = 0xFFA3ADB3L
        const val bubbleIn: Long = 0xFFE9EDEFL
        const val onBubbleIn: Long = 0xFF0E1418L
        const val bubbleInMeta: Long = 0xFF56616AL
        const val primary: Long = 0xFF141A1EL
        const val onPrimary: Long = 0xFFF2F5F6L
        const val lamp: Long = 0xFFF2AE2EL
        const val onLamp: Long = 0xFF1A1405L
        const val selection: Long = 0xFFE3E8EBL
        const val danger: Long = 0xFFB8322AL
        const val onDanger: Long = 0xFFFFFFFFL
        const val focusRing: Long = 0xFF0E1418L
        const val scrim: Long = 0xFF0E1418L
    }

    object DarkColors {
        const val ground: Long = 0xFF0B0F12L
        const val surface: Long = 0xFF11161AL
        const val surfaceRaised: Long = 0xFF192025L
        const val surfaceSunken: Long = 0xFF0B0F12L
        const val hairline: Long = 0xFF232B31L
        const val text: Long = 0xFFECF0F2L
        const val textSecondary: Long = 0xFFA7B1B8L
        const val textTertiary: Long = 0xFF86919AL
        const val bubbleOut: Long = 0xFFE8EDF0L
        const val onBubbleOut: Long = 0xFF0B0F12L
        const val bubbleOutMeta: Long = 0xFF4E5961L
        const val bubbleIn: Long = 0xFF1C2328L
        const val onBubbleIn: Long = 0xFFECF0F2L
        const val bubbleInMeta: Long = 0xFF8E99A1L
        const val primary: Long = 0xFFE8EDF0L
        const val onPrimary: Long = 0xFF0B0F12L
        const val lamp: Long = 0xFFF5BC4CL
        const val onLamp: Long = 0xFF1A1405L
        const val selection: Long = 0xFF1E262BL
        const val danger: Long = 0xFFFF8277L
        const val onDanger: Long = 0xFF1A0705L
        const val focusRing: Long = 0xFFECF0F2L
        const val scrim: Long = 0xFF000000L
    }

    object Space {
        const val s0: Float = 0f
        const val s1: Float = 2f
        const val s2: Float = 4f
        const val s3: Float = 8f
        const val s4: Float = 12f
        const val s5: Float = 16f
        const val s6: Float = 20f
        const val s7: Float = 24f
        const val s8: Float = 32f
        const val s9: Float = 40f
        const val s10: Float = 48f
        const val s11: Float = 64f
    }

    object Size {
        const val avatarS: Float = 28f
        const val avatarM: Float = 44f
        const val avatarL: Float = 56f
        const val avatarXL: Float = 96f
        const val hitMin: Float = 44f
        const val row: Float = 72f
        const val composerMin: Float = 44f
        const val badge: Float = 20f
        const val presenceDot: Float = 10f
        const val lumaLine: Float = 2f
        const val sidebar: Float = 264f
        const val listPane: Float = 340f
        const val bubbleMax: Float = 560f
    }

    object Radius {
        const val xs: Float = 6f
        const val s: Float = 10f
        const val m: Float = 14f
        const val bubble: Float = 20f
        const val bubbleJoined: Float = 6f
        const val sheet: Float = 24f
        const val pill: Float = 999f
    }

    object Typography {
        val largeTitle = TypeStyle(32f, 38f, 600, -0.6f, display = true, tabular = false)
        val title = TypeStyle(22f, 28f, 600, -0.3f, display = true, tabular = false)
        val headline = TypeStyle(16f, 21f, 600, -0.1f, display = false, tabular = false)
        val body = TypeStyle(16f, 22f, 400, 0f, display = false, tabular = false)
        val callout = TypeStyle(15f, 20f, 400, 0f, display = false, tabular = false)
        val subhead = TypeStyle(14f, 19f, 400, 0f, display = false, tabular = false)
        val footnote = TypeStyle(13f, 18f, 400, 0f, display = false, tabular = false)
        val caption = TypeStyle(12f, 16f, 500, 0.1f, display = false, tabular = true)
    }

    object Duration {
        const val instant: Int = 90
        const val quick: Int = 160
        const val standard: Int = 240
        const val emphasized: Int = 380
        const val lumaPulse: Int = 1600
    }

    object Spring {
        val send = SpringSpec(response = 0.35f, damping = 0.82f)
        val sheet = SpringSpec(response = 0.45f, damping = 0.9f)
    }
}
