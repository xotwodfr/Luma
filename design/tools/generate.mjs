// Emits per-platform token files from design/tokens.json.
// Usage: node tools/generate.mjs <outDir>
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const HEADER = 'Generated from design/tokens.json — do not edit.';
const kebab = (s) => s.replace(/[A-Z]/g, (m) => '-' + m.toLowerCase());
const pascal = (s) => s[0].toUpperCase() + s.slice(1);
const hex6 = (h) => h.slice(1).toUpperCase();
const entries = (o) => Object.entries(o);

function css(t) {
  const vars = (theme) => entries(t.color[theme]).map(([k, v]) => `  --luma-color-${kebab(k)}: ${v};`).join('\n');
  const scalar = [
    ...entries(t.space).map(([k, v]) => `  --luma-space-${k}: ${v}px;`),
    ...entries(t.size).map(([k, v]) => `  --luma-size-${kebab(k)}: ${v}px;`),
    ...entries(t.radius).map(([k, v]) => `  --luma-radius-${kebab(k)}: ${v}px;`),
    ...entries(t.type).flatMap(([k, v]) => [
      `  --luma-type-${kebab(k)}-size: ${v.size}px;`,
      `  --luma-type-${kebab(k)}-line: ${v.lineHeight}px;`,
      `  --luma-type-${kebab(k)}-weight: ${v.weight};`,
      `  --luma-type-${kebab(k)}-tracking: ${v.tracking}px;`,
    ]),
    `  --luma-font-display: "${t.font.display}", system-ui, sans-serif;`,
    `  --luma-font-ui: system-ui, sans-serif;`,
    ...entries(t.motion.duration).map(([k, v]) => `  --luma-duration-${kebab(k)}: ${v}ms;`),
    ...entries(t.motion.curve).map(([k, v]) => `  --luma-curve-${kebab(k)}: cubic-bezier(${v.join(', ')});`),
  ].join('\n');
  const shadow = (theme) => {
    const e = t.elevation.overlay;
    return `  --luma-shadow-overlay: 0 ${e.y}px ${e.blur}px rgba(0, 0, 0, ${e.alpha[theme]});`;
  };
  return `/* ${HEADER} */
:root {
  color-scheme: light dark;
${vars('light')}
${shadow('light')}
${scalar}
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
${vars('dark').replace(/^/gm, '  ')}
  ${shadow('dark')}
  }
}
:root[data-theme="dark"] {
${vars('dark')}
${shadow('dark')}
}
/* Scoped themes, for showing both side by side regardless of the page theme. */
.luma-light {
${vars('light')}
${shadow('light')}
  color-scheme: light;
}
.luma-dark {
${vars('dark')}
${shadow('dark')}
  color-scheme: dark;
}
`;
}

function swift(t) {
  const colors = entries(t.color.light)
    .map(([k, v]) => `        public static let ${k} = LumaColor(light: 0x${hex6(v)}, dark: 0x${hex6(t.color.dark[k])})`)
    .join('\n');
  const nums = (o, prefix = '') => entries(o).map(([k, v]) => `        public static let ${prefix}${k}: Double = ${v}`).join('\n');
  const types = entries(t.type)
    .map(([k, v]) => `        public static let ${k} = LumaTypeStyle(size: ${v.size}, lineHeight: ${v.lineHeight}, weight: ${v.weight}, tracking: ${v.tracking}, display: ${v.family === 'display'}, tabular: ${v.numeric === 'tabular'})`)
    .join('\n');
  const springs = entries(t.motion.spring)
    .map(([k, v]) => `        public static let ${k} = (response: ${v.response}, damping: ${v.damping})`)
    .join('\n');
  return `// ${HEADER}

public struct LumaColor: Sendable {
    public let light: UInt32
    public let dark: UInt32
}

public struct LumaTypeStyle: Sendable {
    public let size: Double
    public let lineHeight: Double
    public let weight: Int
    public let tracking: Double
    public let display: Bool
    public let tabular: Bool
}

public enum LumaTokens {
    public static let displayFont = "${t.font.display}"

    public enum Color {
${colors}
    }

    public enum Space {
${nums(t.space, 's')}
    }

    public enum Size {
${nums(t.size)}
    }

    public enum Radius {
${nums(t.radius)}
    }

    public enum Typography {
${types}
    }

    public enum Duration {
${nums(t.motion.duration)}
    }

    public enum Spring {
${springs}
    }

    public enum Elevation {
        public static let overlayY: Double = ${t.elevation.overlay.y}
        public static let overlayBlur: Double = ${t.elevation.overlay.blur}
        public static let overlayAlphaLight: Double = ${t.elevation.overlay.alpha.light}
        public static let overlayAlphaDark: Double = ${t.elevation.overlay.alpha.dark}
    }
}
`;
}

function kotlin(t) {
  const colors = (theme) =>
    entries(t.color[theme]).map(([k, v]) => `        const val ${k}: Long = 0xFF${hex6(v)}L`).join('\n');
  const nums = (o, prefix = '') =>
    entries(o).map(([k, v]) => `        const val ${prefix}${k}: Float = ${v}f`).join('\n');
  const types = entries(t.type)
    .map(([k, v]) => `        val ${k} = TypeStyle(${v.size}f, ${v.lineHeight}f, ${v.weight}, ${v.tracking}f, display = ${v.family === 'display'}, tabular = ${v.numeric === 'tabular'})`)
    .join('\n');
  const springs = entries(t.motion.spring)
    .map(([k, v]) => `        val ${k} = SpringSpec(response = ${v.response}f, damping = ${v.damping}f)`)
    .join('\n');
  return `// ${HEADER}
package app.luma.design

object LumaTokens {
    const val DISPLAY_FONT = "${t.font.display}"

    data class TypeStyle(val size: Float, val lineHeight: Float, val weight: Int, val tracking: Float, val display: Boolean, val tabular: Boolean)
    data class SpringSpec(val response: Float, val damping: Float)

    object LightColors {
${colors('light')}
    }

    object DarkColors {
${colors('dark')}
    }

    object Space {
${nums(t.space, 's')}
    }

    object Size {
${nums(t.size)}
    }

    object Radius {
${nums(t.radius)}
    }

    object Typography {
${types}
    }

    object Duration {
${entries(t.motion.duration).map(([k, v]) => `        const val ${k}: Int = ${v}`).join('\n')}
    }

    object Spring {
${springs}
    }
}
`;
}

function xaml(t) {
  const theme = (key, name) =>
    `        <ResourceDictionary x:Key="${key}">
${entries(t.color[name])
  .map(([k, v]) => `            <Color x:Key="Luma${pascal(k)}Color">#FF${hex6(v)}</Color>
            <SolidColorBrush x:Key="Luma${pascal(k)}Brush" Color="#FF${hex6(v)}" />`)
  .join('\n')}
        </ResourceDictionary>`;
  return `<!-- ${HEADER} -->
<ResourceDictionary
    xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"
    xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml">
    <ResourceDictionary.ThemeDictionaries>
${theme('Light', 'light')}
${theme('Default', 'dark')}
    </ResourceDictionary.ThemeDictionaries>

${entries(t.space).map(([k, v]) => `    <x:Double x:Key="LumaSpace${k}">${v}</x:Double>`).join('\n')}
${entries(t.size).map(([k, v]) => `    <x:Double x:Key="LumaSize${pascal(k)}">${v}</x:Double>`).join('\n')}
${entries(t.radius).map(([k, v]) => `    <CornerRadius x:Key="LumaRadius${pascal(k)}">${v}</CornerRadius>`).join('\n')}
${entries(t.type).map(([k, v]) => `    <x:Double x:Key="LumaType${pascal(k)}Size">${v.size}</x:Double>`).join('\n')}
    <FontFamily x:Key="LumaDisplayFont">ms-appx:///Assets/Fonts/InstrumentSans.ttf#${t.font.display}</FontFamily>
</ResourceDictionary>
`;
}

function qml(t) {
  const colors = entries(t.color.light)
    .map(([k, v]) => `    readonly property color ${k}: dark ? "${t.color.dark[k]}" : "${v}"`)
    .join('\n');
  const group = (name, o, prefix = '') =>
    `    readonly property QtObject ${name}: QtObject {
${entries(o).map(([k, v]) => `        readonly property real ${prefix}${k}: ${v}`).join('\n')}
    }`;
  const types = `    readonly property QtObject type: QtObject {
${entries(t.type)
  .map(([k, v]) => `        readonly property var ${k}: ({ size: ${v.size}, lineHeight: ${v.lineHeight}, weight: ${v.weight}, tracking: ${v.tracking}, display: ${v.family === 'display'}, tabular: ${v.numeric === 'tabular'} })`)
  .join('\n')}
    }`;
  return `// ${HEADER}
pragma Singleton
import QtQuick

QtObject {
    property bool dark: false
    readonly property string displayFont: "${t.font.display}"
    // Empty means the platform theme's UI font (Noto Sans on KDE, Cantarell on GNOME).
    readonly property string uiFont: ""

${colors}

${group('space', t.space, 's')}
${group('size', t.size)}
${group('radius', t.radius)}
${group('duration', t.motion.duration)}
${types}
}
`;
}

export function generate(tokens, outDir) {
  mkdirSync(outDir, { recursive: true });
  const out = {
    'luma-tokens.css': css(tokens),
    'LumaTokens.swift': swift(tokens),
    'LumaTokens.kt': kotlin(tokens),
    'LumaTokens.xaml': xaml(tokens),
    'LumaTokens.qml': qml(tokens),
    qmldir: 'module Luma.Tokens\nsingleton LumaTokens 1.0 LumaTokens.qml\n',
  };
  for (const [name, src] of entries(out)) writeFileSync(join(outDir, name), src);
  return Object.keys(out);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const tokens = JSON.parse(readFileSync(new URL('../tokens.json', import.meta.url)));
  const dir = process.argv[2] ?? 'generated';
  console.log(generate(tokens, dir).map((f) => join(dir, f)).join('\n'));
}
