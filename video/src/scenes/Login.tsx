import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  { text: "nostr login", delay: 15, typewriter: true, color: COLORS.command, prefix: "prompt" },
  { text: "Enter your nsec (leave blank to generate):", delay: 55, color: COLORS.text },
  { text: "", delay: 70, color: COLORS.text }, // blank line pause
  { text: "Generating new keypair...", delay: 85, color: COLORS.textDim },
  { text: "", delay: 120 },
  { text: "✓ Account created", delay: 130, color: COLORS.success },
  { text: "npub: npub1x7dk...3qf", delay: 145, color: COLORS.npub, indent: 1 },
  { text: "Relays: 6 configured", delay: 158, color: COLORS.textDim, indent: 1 },
];

export const Login: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.login.start);
  return <TerminalRenderer lines={rendered} />;
};
