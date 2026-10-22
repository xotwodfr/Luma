import QtQuick
import QtQuick.Controls.Basic
import Luma

// Primary actions use the same luminance inversion as your own messages.
Button {
    id: control
    property bool primary: true
    property string iconName

    focusPolicy: Qt.StrongFocus
    hoverEnabled: true
    leftPadding: 16
    rightPadding: 16
    topPadding: 9
    bottomPadding: 9
    font.pixelSize: LumaTokens.type.subhead.size
    font.weight: Font.Medium

    contentItem: Item {
        implicitWidth: label.implicitWidth
        implicitHeight: label.implicitHeight
        Row {
            id: label
            anchors.centerIn: parent
            spacing: 8
            Icon {
                visible: control.iconName !== ""
                name: control.iconName
                size: 16
                tint: control.primary ? LumaTokens.textOnPrimary : LumaTokens.text
                anchors.verticalCenter: parent.verticalCenter
            }
            Text {
                textFormat: Text.PlainText
                text: control.text
                font: control.font
                color: control.primary ? LumaTokens.textOnPrimary : LumaTokens.text
                anchors.verticalCenter: parent.verticalCenter
            }
        }
    }
    background: Rectangle {
        implicitHeight: 38
        radius: LumaTokens.radius.s
        color: control.primary ? LumaTokens.primary : (control.hovered ? LumaTokens.selection : "transparent")
        opacity: control.enabled ? (control.down ? 0.85 : 1) : 0.5
        border.width: control.visualFocus ? 2 : (control.primary ? 0 : 1)
        border.color: control.visualFocus ? LumaTokens.focusRing : LumaTokens.hairline
        Rectangle { // focus ring offset 2px, visible on the inverted fill
            visible: control.visualFocus && control.primary
            anchors.fill: parent
            anchors.margins: -4
            radius: parent.radius + 4
            color: "transparent"
            border.width: 2
            border.color: LumaTokens.focusRing
        }
    }
}
