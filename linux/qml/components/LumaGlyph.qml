import QtQuick
import Luma

// The Luma mark: a lit disc with the lamp at its shoulder.
Item {
    property real scale_: width / 44
    width: 44
    height: 44
    Rectangle { x: 4 * parent.scale_; y: 6 * parent.scale_; width: 34 * parent.scale_; height: width; radius: width / 2; color: LumaTokens.primary }
    Rectangle { x: 25.5 * parent.scale_; y: 1.5 * parent.scale_; width: 17 * parent.scale_; height: width; radius: width / 2; color: LumaTokens.ground }
    Rectangle { x: 28.5 * parent.scale_; y: 4.5 * parent.scale_; width: 11 * parent.scale_; height: width; radius: width / 2; color: LumaTokens.lamp }
}
