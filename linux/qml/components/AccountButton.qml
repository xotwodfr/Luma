import QtQuick
import QtQuick.Controls.Basic
import Luma

// Who you are, where you're signed in, and Sign out.
AbstractButton {
    id: meButton
    property bool menuToRight: false

    width: 36
    height: 36
    focusPolicy: Qt.StrongFocus
    Accessible.name: "Account"
    onClicked: menu.open()
    contentItem: Avatar { size: 32; initials: App.me.initials || "" }
    background: Rectangle { radius: 18; color: "transparent"; border.width: meButton.visualFocus ? 2 : 0; border.color: LumaTokens.focusRing }

    Popup {
        id: menu
        x: meButton.menuToRight ? meButton.width + 8 : meButton.width - implicitWidth
        y: meButton.menuToRight ? meButton.height - implicitHeight : meButton.height + 6
        padding: 6
        background: Rectangle { radius: LumaTokens.radius.m; color: LumaTokens.surfaceRaised; border.color: LumaTokens.hairline }
        contentItem: Column {
            spacing: 2
            Column {
                padding: 10
                spacing: 2
                Text { text: App.me.displayName || ""; color: LumaTokens.text; font.weight: Font.DemiBold; font.pixelSize: LumaTokens.type.subhead.size }
                Text { text: "@" + (App.me.username || "") + " on " + App.serverHost; color: LumaTokens.textTertiary; font.pixelSize: LumaTokens.type.caption.size }
            }
            Rectangle { width: 220; height: 1; color: LumaTokens.hairline }
            LumaButton {
                width: 220
                primary: false
                text: "Sign out"
                iconName: "log-out"
                onClicked: { menu.close(); App.signOut() }
            }
        }
    }
}
