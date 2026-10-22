import QtQuick
import QtQuick.Controls.Basic
import Luma
import "components"

Rectangle {
    id: view
    property bool showBack: false
    signal back()
    signal newConversation()
    readonly property var h: App.header

    color: LumaTokens.surface

    // Nothing open (desktop): a quiet invitation, not an illustration.
    EmptyState {
        visible: App.current === ""
        anchors.centerIn: parent
        centered: true
        heading: "Pick a conversation"
        body: "Choose one from the list, or press Ctrl+N to start a new one."
    }

    Item {
        anchors.fill: parent
        visible: App.current !== ""

        Item {
            id: head
            width: parent.width
            height: 60
            IconButton {
                id: backButton
                visible: view.showBack
                iconName: "chevron-left"
                tint: LumaTokens.text
                tip: "Back"
                anchors { left: parent.left; leftMargin: 8; verticalCenter: parent.verticalCenter }
                onClicked: view.back()
            }
            Avatar {
                id: headAvatar
                anchors { left: view.showBack ? backButton.right : parent.left; leftMargin: view.showBack ? 4 : 20; verticalCenter: parent.verticalCenter }
                size: 36
                initials: view.h.initials || ""
                pair: view.h.kind === "group" ? view.h.pair : []
                online: view.h.online || false
            }
            Column {
                anchors { left: headAvatar.right; leftMargin: 12; right: parent.right; rightMargin: 20; verticalCenter: parent.verticalCenter }
                spacing: 1
                Text {
                    text: view.h.title || ""
                    width: parent.width
                    elide: Text.ElideRight
                    color: LumaTokens.text
                    font.pixelSize: LumaTokens.type.headline.size - 1
                    font.weight: Font.DemiBold
                }
                Text {
                    visible: text !== ""
                    text: view.h.status || ""
                    color: LumaTokens.textTertiary
                    font.pixelSize: LumaTokens.type.caption.size
                }
            }
            Rectangle { height: 1; color: LumaTokens.hairline; anchors { left: parent.left; right: parent.right; bottom: parent.bottom } }
            LumaLine { anchors { left: parent.left; right: parent.right; bottom: parent.bottom } }
        }
        StatusNote {
            id: note
            anchors { top: head.bottom; left: parent.left; right: parent.right }
            height: visible ? implicitHeight : 0
        }

        ListView {
            id: messages
            property bool stick: true
            anchors { top: note.bottom; left: parent.left; right: parent.right; bottom: composer.top; leftMargin: 20; rightMargin: 20 }
            clip: true
            model: App.messages
            delegate: MessageDelegate {}
            topMargin: 16
            bottomMargin: 8
            boundsBehavior: Flickable.StopAtBounds
            footer: TypingBubble { active: view.h.typing || false }
            ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
            onMovementEnded: stick = atYEnd
            onCountChanged: if (stick) Qt.callLater(positionViewAtEnd)
            onHeightChanged: if (stick) Qt.callLater(positionViewAtEnd)
            Accessible.role: Accessible.List
            Accessible.name: "Messages"
            Connections {
                target: App
                function onCurrentChanged() { messages.stick = true; Qt.callLater(messages.positionViewAtEnd) }
            }
        }
        EmptyState {
            visible: messages.count === 0
            anchors.centerIn: messages
            centered: true
            heading: "Say hello to " + (view.h.firstName || "")
            body: "Messages you send appear here."
        }

        Composer {
            id: composer
            anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
            onSubmitted: (text) => { messages.stick = true; App.send(text) }
            Connections {
                target: App
                function onCurrentChanged() { composer.focusInput() }
            }
        }
    }
}
