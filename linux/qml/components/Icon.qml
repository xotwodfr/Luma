import QtQuick
import Luma

// A Lucide-style icon rendered by the C++ provider in any token color.
Image {
    id: root
    property string name
    property color tint: LumaTokens.text
    property int size: 20

    width: size
    height: size
    sourceSize: Qt.size(size * Screen.devicePixelRatio, size * Screen.devicePixelRatio)
    source: name ? "image://icon/" + name + "?color=" + encodeURIComponent(tint.toString()) : ""
    fillMode: Image.PreserveAspectFit
    smooth: true
    Accessible.ignored: true
}
