import QtQuick
import Luma

// The words under the luma line: exactly what is happening with the connection.
Text {
    textFormat: Text.PlainText
    readonly property string conn: App.connection
    visible: conn !== "online"
    text: conn === "offline" ? "Offline. Messages will send when you’re back."
        : conn === "reconnecting" ? (App.retryIn > 0 ? "Reconnecting in " + App.retryIn + " s" : "Reconnecting…")
        : "Connecting…"
    color: LumaTokens.textSecondary
    font.pixelSize: LumaTokens.type.caption.size
    font.features: { "tnum": 1 }
    horizontalAlignment: Text.AlignHCenter
    topPadding: 6
    bottomPadding: 6
    Accessible.role: Accessible.AlertMessage
    Accessible.name: text
}
