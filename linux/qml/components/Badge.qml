import QtQuick
import Luma

// Unread count. The lamp means "needs your attention"; muted conversations get a quiet outline.
Rectangle {
    id: root
    property int count
    property bool quiet

    implicitWidth: Math.max(LumaTokens.size.badge, label.implicitWidth + 12)
    implicitHeight: LumaTokens.size.badge
    radius: height / 2
    color: quiet ? "transparent" : LumaTokens.lamp
    border.width: quiet ? 1.5 : 0
    border.color: LumaTokens.hairline

    Text {
        textFormat: Text.PlainText
        id: label
        anchors.centerIn: parent
        text: root.count > 99 ? "99+" : String(root.count)
        color: root.quiet ? LumaTokens.textTertiary : LumaTokens.textOnLamp
        font.pixelSize: LumaTokens.type.caption.size
        font.weight: Font.DemiBold
        font.features: { "tnum": 1 }
    }
}
