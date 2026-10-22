import QtQuick
import QtQuick.Controls.Basic
import Luma
import "components"

Rectangle {
    id: rail
    color: LumaTokens.ground

    Column {
        anchors { top: parent.top; topMargin: 12; horizontalCenter: parent.horizontalCenter }
        spacing: 4
        Item {
            width: 40
            height: 40
            Rectangle { anchors.fill: parent; radius: LumaTokens.radius.xs; color: LumaTokens.selection }
            Rectangle { width: 3; radius: 1.5; color: LumaTokens.lamp; anchors { left: parent.left; leftMargin: -8; top: parent.top; bottom: parent.bottom; topMargin: 11; bottomMargin: 11 } }
            Icon { anchors.centerIn: parent; name: "message-circle"; tint: LumaTokens.text }
            Accessible.role: Accessible.PageTab
            Accessible.name: "Chats"
        }
    }

    AccountButton {
        anchors { bottom: parent.bottom; bottomMargin: 14; horizontalCenter: parent.horizontalCenter }
        menuToRight: true
    }
    Rectangle { width: 1; anchors { right: parent.right; top: parent.top; bottom: parent.bottom } color: LumaTokens.hairline }
}
