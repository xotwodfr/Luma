import QtQuick
import QtQuick.Controls.Basic
import Luma
import "components"

Popup {
    id: dialog
    modal: true
    focus: true
    anchors.centerIn: Overlay.overlay
    width: 380
    padding: 24
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
    property string error
    property bool busy

    onOpened: { error = ""; busy = false; username.text = ""; username.forceActiveFocus() }

    function submit() {
        if (busy)
            return
        busy = true
        error = ""
        App.startDirect(username.text)
    }
    Connections {
        target: App
        function onStartDirectFinished(ok, message) {
            dialog.busy = false
            if (ok)
                dialog.close()
            else
                dialog.error = message
        }
    }

    Overlay.modal: Rectangle { color: Qt.alpha(LumaTokens.scrim, LumaTokens.dark ? 0.6 : 0.4) }
    background: Rectangle {
        radius: LumaTokens.radius.sheet
        color: LumaTokens.surfaceRaised
        border.color: LumaTokens.hairline
    }
    contentItem: Column {
        spacing: 14
        Text {
            textFormat: Text.PlainText
            text: "New conversation"
            color: LumaTokens.text
            font.family: LumaTokens.displayFont
            font.pixelSize: LumaTokens.type.title.size
            font.weight: Font.DemiBold
            font.letterSpacing: LumaTokens.type.title.tracking
        }
        Text {
            textFormat: Text.PlainText
            text: "Enter their exact username."
            color: LumaTokens.textSecondary
            font.pixelSize: LumaTokens.type.subhead.size
        }
        LumaTextField {
            id: username
            width: parent.width
            placeholderText: "Username"
            iconName: "user"
            onAccepted: dialog.submit()
        }
        Text {
            textFormat: Text.PlainText
            visible: dialog.error !== ""
            text: dialog.error
            color: LumaTokens.danger
            font.pixelSize: LumaTokens.type.footnote.size
            Accessible.role: Accessible.AlertMessage
            Accessible.name: text
        }
        Row {
            anchors.right: parent.right
            spacing: 8
            LumaButton { primary: false; text: "Cancel"; onClicked: dialog.close() }
            LumaButton { text: dialog.busy ? "Starting…" : "Start conversation"; enabled: username.text.trim() !== "" && !dialog.busy; onClicked: dialog.submit() }
        }
    }
}
