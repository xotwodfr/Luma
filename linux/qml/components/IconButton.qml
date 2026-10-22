import QtQuick
import QtQuick.Controls.Basic
import Luma

AbstractButton {
    id: control
    property string iconName
    property color tint: LumaTokens.textSecondary
    property string tip

    implicitWidth: 36
    implicitHeight: 36
    focusPolicy: Qt.StrongFocus
    hoverEnabled: true
    Accessible.name: tip
    Accessible.role: Accessible.Button

    background: Rectangle {
        radius: LumaTokens.radius.s
        color: control.down ? LumaTokens.selection : control.hovered ? Qt.alpha(LumaTokens.selection, 0.7) : "transparent"
        border.width: control.visualFocus ? 2 : 0
        border.color: LumaTokens.focusRing
        Behavior on color { ColorAnimation { duration: LumaTokens.duration.instant } }
    }
    contentItem: Item {
        Icon {
            anchors.centerIn: parent
            name: control.iconName
            tint: control.enabled ? control.tint : LumaTokens.textTertiary
        }
    }
    ToolTip.visible: hovered && tip !== ""
    ToolTip.text: tip
    ToolTip.delay: 700
}
