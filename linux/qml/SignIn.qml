import QtQuick
import QtQuick.Controls.Basic
import Luma
import "components"

Rectangle {
    id: page
    property bool creating: false
    color: LumaTokens.surface // inputs are surfaceSunken, which only reads against surface (in dark, sunken == ground)

    function submit() {
        if (creating)
            App.signUp(invite.text, username.text, displayName.text, password.text)
        else
            App.signIn(username.text, password.text)
    }

    Column {
        anchors.centerIn: parent
        width: Math.min(340, page.width - 48)
        spacing: 14

        Row {
            spacing: 12
            LumaGlyph { width: 36; height: 36; anchors.verticalCenter: parent.verticalCenter }
            Text {
                text: "Luma"
                color: LumaTokens.text
                font.family: LumaTokens.displayFont
                font.pixelSize: 38
                font.weight: Font.DemiBold
                font.letterSpacing: -1.2
                anchors.verticalCenter: parent.verticalCenter
            }
        }
        Text {
            text: page.creating ? "Create your account with an invite code." : "Sign in to continue."
            color: LumaTokens.textSecondary
            font.pixelSize: LumaTokens.type.subhead.size
            bottomPadding: 10
        }
        Text {
            visible: App.notice !== ""
            text: App.notice
            width: parent.width
            wrapMode: Text.Wrap
            color: LumaTokens.textSecondary
            font.pixelSize: LumaTokens.type.footnote.size
            Accessible.role: Accessible.AlertMessage
        }
        LumaTextField { id: invite; visible: page.creating; width: parent.width; placeholderText: "Invite code"; onAccepted: username.forceActiveFocus() }
        LumaTextField { id: username; width: parent.width; placeholderText: "Username"; focus: true; onAccepted: (page.creating ? displayName : password).forceActiveFocus() }
        LumaTextField { id: displayName; visible: page.creating; width: parent.width; placeholderText: "Your name"; onAccepted: password.forceActiveFocus() }
        LumaTextField { id: password; width: parent.width; placeholderText: "Password"; echoMode: TextInput.Password; onAccepted: page.submit() }
        Text {
            visible: App.errorText !== ""
            text: App.errorText
            width: parent.width
            wrapMode: Text.Wrap
            color: LumaTokens.danger
            font.pixelSize: LumaTokens.type.footnote.size
            Accessible.role: Accessible.AlertMessage
            Accessible.name: text
        }
        LumaButton {
            width: parent.width
            enabled: App.phase !== "signingIn"
            text: App.phase === "signingIn" ? (page.creating ? "Creating account…" : "Signing in…") : (page.creating ? "Create account" : "Sign in")
            onClicked: page.submit()
        }
        LumaButton {
            width: parent.width
            primary: false
            text: page.creating ? "I already have an account" : "I have an invite code"
            onClicked: { page.creating = !page.creating; App.clearError() }
        }
    }
    Column {
        anchors { bottom: parent.bottom; bottomMargin: 20; horizontalCenter: parent.horizontalCenter }
        spacing: 2
        Text {
            anchors.horizontalCenter: parent.horizontalCenter
            text: "Signing in to " + App.serverHost
            color: LumaTokens.textTertiary
            font.pixelSize: LumaTokens.type.caption.size
        }
        Text {
            anchors.horizontalCenter: parent.horizontalCenter
            text: "Messages are encrypted in transit, not end to end."
            color: LumaTokens.textTertiary
            font.pixelSize: LumaTokens.type.caption.size
        }
    }
}
