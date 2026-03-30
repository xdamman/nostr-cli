import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: "nostr follow fiatjaf@nostr.com --alias fiatjaf",
    delay: 15,
    typewriter: true,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "✓ Following fiatjaf (npub1sg6...e2uf)", delay: 65, color: COLORS.success },
  { text: "", delay: 80 },
  {
    text: "nostr profile fiatjaf -n 5",
    delay: 100,
    typewriter: true,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "Name:    fiatjaf", delay: 140, color: COLORS.text, indent: 1 },
  { text: "NIP-05:  fiatjaf@nostr.com ✓", delay: 150, color: COLORS.text, indent: 1 },
  { text: "About:   creating nostr and other things", delay: 160, color: COLORS.textDim, indent: 1 },
  { text: "", delay: 172 },
  { text: "30/03 10:15  working on a new relay implementation", delay: 182, color: COLORS.textDim, indent: 1 },
  { text: "30/03 09:42  just shipped NIP-XX support", delay: 192, color: COLORS.textDim, indent: 1 },
  { text: "29/03 22:10  the protocol is the product", delay: 202, color: COLORS.textDim, indent: 1 },
  { text: "29/03 18:30  testing gift-wrapped DMs", delay: 212, color: COLORS.textDim, indent: 1 },
  { text: "29/03 15:05  nostr is inevitable", delay: 222, color: COLORS.textDim, indent: 1 },
];

export const Follow: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.follow.start);
  return <TerminalRenderer lines={rendered} />;
};
