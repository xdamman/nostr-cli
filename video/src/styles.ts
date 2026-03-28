export const COLORS = {
  bg: "#0d1117",
  windowChrome: "#1c2128",
  windowBorder: "#30363d",
  text: "#e6edf3",
  textDim: "#7d8590",
  prompt: "#3fb950",
  command: "#79c0ff",
  success: "#3fb950",
  warning: "#d29922",
  label: "#58a6ff",
  npub: "#7d8590",
  json: "#e6edf3",
  jsonKey: "#ff7b72",
  jsonString: "#a5d6ff",
  red: "#ff7b72",
  yellow: "#d29922",
  green: "#3fb950",
  cyan: "#58a6ff",
} as const;

export const FONT = {
  family: "'JetBrains Mono', 'SF Mono', 'Fira Code', 'Cascadia Code', Consolas, 'Courier New', monospace",
  size: 18,
  lineHeight: 1.6,
} as const;

export const FPS = 30;
export const DURATION_FRAMES = 1650; // ~55 seconds

// Scene timing in frames
export const SCENES = {
  login:    { start: 0,    end: 240 },   // 0-8s
  post:     { start: 240,  end: 480 },   // 8-16s
  follow:   { start: 480,  end: 780 },   // 16-26s
  dm:       { start: 780,  end: 1020 },  // 26-34s
  accounts: { start: 1020, end: 1260 },  // 34-42s
  botMode:  { start: 1260, end: 1650 },  // 42-55s
} as const;
