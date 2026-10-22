import QtQuick
import QtQuick.Controls.Basic
import Luma

ApplicationWindow {
    id: window

    required property string forcedTheme
    readonly property bool wide: width >= 1100
    readonly property bool narrow: width < 720

    visible: true
    minimumWidth: 420
    minimumHeight: 480
    title: App.current !== "" && App.header.title ? App.header.title + " — Luma" : "Luma"
    color: LumaTokens.ground
    font.pixelSize: LumaTokens.type.subhead.size

    Binding {
        target: LumaTokens
        property: "dark"
        value: window.forcedTheme === "dark"
               || (window.forcedTheme === "system" && Application.styleHints.colorScheme === Qt.ColorScheme.Dark)
    }

    Loader {
        anchors.fill: parent
        sourceComponent: App.phase === "ready" ? shell : signIn
    }

    Component { id: signIn; SignIn {} }

    Component {
        id: shell
        Item {
            Shortcut { sequence: "Ctrl+N"; onActivated: newConversation.open() }
            Shortcut { sequence: "Ctrl+K"; onActivated: { if (window.narrow) App.openConversation(""); chats.focusSearch() } }

            Rail {
                id: rail
                visible: window.wide
                width: visible ? 56 : 0
                anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
            }
            ChatsPane {
                id: chats
                visible: !window.narrow || App.current === ""
                width: window.narrow ? parent.width : LumaTokens.size.listPane - 20
                showAccount: !window.wide
                anchors { left: rail.right; top: parent.top; bottom: parent.bottom }
                onNewConversation: newConversation.open()
            }
            ChatView {
                visible: !window.narrow || App.current !== ""
                showBack: window.narrow
                anchors { left: window.narrow ? parent.left : chats.right; right: parent.right; top: parent.top; bottom: parent.bottom }
                onBack: App.openConversation("")
            }
            NewConversation { id: newConversation }
        }
    }
}
