import QtQuick
import QtQuick.Controls.Basic
import Luma
import "components"

Rectangle {
    id: pane
    property bool showAccount: false
    signal newConversation()
    function focusSearch() { search.forceActiveFocus(); search.selectAll() }

    color: LumaTokens.surface

    Item {
        id: head
        width: parent.width
        height: 56
        Text {
            text: "Chats"
            anchors { left: parent.left; leftMargin: 16; verticalCenter: parent.verticalCenter }
            color: LumaTokens.text
            font.family: LumaTokens.displayFont
            font.pixelSize: LumaTokens.type.title.size - 2
            font.weight: Font.DemiBold
            font.letterSpacing: LumaTokens.type.title.tracking
        }
        AccountButton {
            id: account
            visible: pane.showAccount
            anchors { right: compose.left; rightMargin: 4; verticalCenter: parent.verticalCenter }
        }
        IconButton {
            id: compose
            anchors { right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
            iconName: "square-pen"
            tint: LumaTokens.text
            tip: "New conversation (Ctrl+N)"
            onClicked: pane.newConversation()
        }
    }
    LumaTextField {
        id: search
        anchors { top: head.bottom; left: parent.left; right: parent.right; leftMargin: 14; rightMargin: 14 }
        iconName: "search"
        placeholderText: "Search or jump to"
        onTextChanged: App.conversations.filter = text
        Keys.onEscapePressed: { text = ""; focus = false }
        Keys.onReturnPressed: list.currentIndex >= 0 || list.count > 0 ? (list.itemAtIndex(0) && list.itemAtIndex(0).clicked()) : undefined
        Text {
            anchors { right: parent.right; rightMargin: 10; verticalCenter: parent.verticalCenter }
            visible: !search.activeFocus && search.text === ""
            text: "Ctrl+K"
            color: LumaTokens.textTertiary
            font.pixelSize: LumaTokens.type.caption.size
        }
    }

    ListView {
        id: list
        anchors { top: search.bottom; topMargin: 8; left: parent.left; right: parent.right; bottom: parent.bottom }
        clip: true
        model: App.conversations
        delegate: ConversationRow {}
        boundsBehavior: Flickable.StopAtBounds
        keyNavigationEnabled: true
        visible: count > 0
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
        Accessible.role: Accessible.List
        Accessible.name: "Conversations"
    }

    // First sync with nothing cached: quiet skeleton rows, the luma line carries the progress.
    Column {
        anchors { top: search.bottom; topMargin: 16; left: parent.left; right: parent.right }
        visible: !App.synced && list.count === 0
        spacing: 0
        Repeater {
            model: 6
            Item {
                width: parent.width
                height: LumaTokens.size.row
                Rectangle { x: 14; anchors.verticalCenter: parent.verticalCenter; width: 44; height: 44; radius: 22; color: LumaTokens.surfaceSunken }
                Rectangle { x: 70; y: 22; width: parent.width * 0.38; height: 10; radius: 5; color: LumaTokens.surfaceSunken }
                Rectangle { x: 70; y: 42; width: parent.width * 0.62; height: 9; radius: 4.5; color: LumaTokens.surfaceSunken }
            }
        }
    }

    EmptyState {
        visible: App.synced && list.count === 0
        anchors { left: parent.left; right: parent.right; verticalCenter: parent.verticalCenter; leftMargin: 24; rightMargin: 24 }
        heading: App.conversations.filter ? "No matches" : "No conversations yet"
        body: App.conversations.filter ? "No conversation matches “" + App.conversations.filter + "”." : "Start one with someone’s username."
        action: App.conversations.filter ? "" : "New conversation"
        actionIcon: "square-pen"
        onActivated: pane.newConversation()
    }

    Rectangle { width: 1; anchors { right: parent.right; top: parent.top; bottom: parent.bottom } color: LumaTokens.hairline }
}
