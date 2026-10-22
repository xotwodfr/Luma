import QtQuick
import QtQuick.Controls.Basic
import Luma

TextField {
    id: field
    property string iconName

    color: LumaTokens.text
    placeholderTextColor: LumaTokens.textTertiary
    selectionColor: LumaTokens.primary
    selectedTextColor: LumaTokens.textOnPrimary
    font.pixelSize: LumaTokens.type.callout.size
    leftPadding: iconName ? 36 : 12
    rightPadding: 12
    topPadding: 9
    bottomPadding: 9
    Accessible.name: placeholderText

    background: Rectangle {
        implicitHeight: 38
        radius: LumaTokens.radius.s
        color: LumaTokens.surfaceSunken
        border.width: field.activeFocus ? 2 : 0
        border.color: LumaTokens.focusRing
        Icon {
            visible: field.iconName !== ""
            name: field.iconName
            size: 17
            tint: LumaTokens.textTertiary
            anchors.verticalCenter: parent.verticalCenter
            x: 12
        }
    }
}
