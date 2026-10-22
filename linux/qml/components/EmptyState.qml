import QtQuick
import Luma

// A display-face heading, one sentence, and at most one action (DESIGN.md §7).
Column {
    id: root
    property string heading
    property string body
    property string action
    property string actionIcon
    property bool centered: false
    signal activated()

    spacing: 10
    Text {
        text: root.heading
        color: LumaTokens.text
        font.family: LumaTokens.displayFont
        font.pixelSize: LumaTokens.type.title.size
        font.weight: Font.DemiBold
        font.letterSpacing: LumaTokens.type.title.tracking
        anchors.horizontalCenter: root.centered ? parent.horizontalCenter : undefined
    }
    Text {
        text: root.body
        color: LumaTokens.textSecondary
        font.pixelSize: LumaTokens.type.subhead.size
        width: Math.min(implicitWidth, 320)
        wrapMode: Text.Wrap
        horizontalAlignment: root.centered ? Text.AlignHCenter : Text.AlignLeft
        anchors.horizontalCenter: root.centered ? parent.horizontalCenter : undefined
    }
    Item { width: 1; height: 4; visible: root.action !== "" }
    LumaButton {
        visible: root.action !== ""
        text: root.action
        iconName: root.actionIcon
        onClicked: root.activated()
        anchors.horizontalCenter: root.centered ? parent.horizontalCenter : undefined
    }
}
