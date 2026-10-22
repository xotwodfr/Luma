import QtQuick
import QtQuick.Controls.Basic
import Luma

ItemDelegate {
    id: row
    required property int index
    required property string conversationId
    required property string title
    required property string initials
    required property var pairInitials
    required property string preview
    required property string time
    required property int unread
    required property bool muted
    required property bool online
    required property bool typing
    required property bool selected
    required property string kind
    required property string accessibleText

    width: ListView.view ? ListView.view.width : 320
    height: LumaTokens.size.row
    padding: 0
    hoverEnabled: true
    focusPolicy: Qt.StrongFocus
    Accessible.role: Accessible.ListItem
    Accessible.name: accessibleText
    onClicked: App.openConversation(conversationId)
    Keys.onReturnPressed: App.openConversation(conversationId)

    background: Rectangle {
        color: row.selected ? LumaTokens.selection : row.hovered ? Qt.alpha(LumaTokens.selection, 0.55) : "transparent"
        Behavior on color { ColorAnimation { duration: LumaTokens.duration.instant } }
        Rectangle { // the lamp marks where you are
            visible: row.selected
            width: 3
            radius: 1.5
            color: LumaTokens.lamp
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom; topMargin: 14; bottomMargin: 14 }
        }
        Rectangle {
            visible: row.index > 0 && !row.selected
            height: 1
            color: LumaTokens.hairline
            anchors { top: parent.top; left: parent.left; right: parent.right; leftMargin: 70 }
        }
        Rectangle {
            anchors.fill: parent
            anchors.margins: 2
            radius: 6
            color: "transparent"
            border.width: row.visualFocus ? 2 : 0
            border.color: LumaTokens.focusRing
        }
    }

    contentItem: Item {
        Avatar {
            id: avatar
            x: 14
            anchors.verticalCenter: parent.verticalCenter
            size: LumaTokens.size.avatarM
            initials: row.initials
            pair: row.kind === "group" ? row.pairInitials : []
            online: row.online
        }
        Row {
            id: nameRow
            spacing: 6
            anchors { left: avatar.right; leftMargin: 12; right: timeLabel.left; rightMargin: 8; top: parent.top; topMargin: 14 }
            Text {
                text: row.title
                width: Math.min(implicitWidth, nameRow.width - (muteIcon.visible ? 20 : 0))
                elide: Text.ElideRight
                color: LumaTokens.text
                font.pixelSize: LumaTokens.type.headline.size - 1
                font.weight: Font.DemiBold
            }
            Icon {
                id: muteIcon
                visible: row.muted
                name: "bell-off"
                size: 14
                tint: LumaTokens.textTertiary
                anchors.verticalCenter: parent.verticalCenter
            }
        }
        Text {
            id: timeLabel
            anchors { right: parent.right; rightMargin: 14; top: parent.top; topMargin: 16 }
            text: row.time
            color: LumaTokens.textTertiary
            font.pixelSize: LumaTokens.type.caption.size
            font.weight: Font.Medium
            font.features: { "tnum": 1 }
        }
        Text {
            anchors { left: nameRow.left; right: badge.visible ? badge.left : parent.right; rightMargin: badge.visible ? 8 : 14; top: nameRow.bottom; topMargin: 3 }
            text: row.preview
            elide: Text.ElideRight
            maximumLineCount: 1
            textFormat: Text.PlainText
            color: row.typing ? LumaTokens.textTertiary : LumaTokens.textSecondary
            font.pixelSize: LumaTokens.type.footnote.size
        }
        Badge {
            id: badge
            visible: row.unread > 0
            count: row.unread
            quiet: row.muted
            anchors { right: parent.right; rightMargin: 14; bottom: parent.bottom; bottomMargin: 14 }
        }
    }
}
