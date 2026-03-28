import React from "react";
import { COLORS, FONT } from "./styles";

interface TerminalWindowProps {
  children: React.ReactNode;
}

export const TerminalWindow: React.FC<TerminalWindowProps> = ({ children }) => {
  return (
    <div
      style={{
        width: "100%",
        height: "100%",
        backgroundColor: "#010409",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 40,
      }}
    >
      <div
        style={{
          width: "100%",
          height: "100%",
          backgroundColor: COLORS.bg,
          borderRadius: 12,
          border: `1px solid ${COLORS.windowBorder}`,
          overflow: "hidden",
          display: "flex",
          flexDirection: "column",
          boxShadow: "0 20px 60px rgba(0,0,0,0.5)",
        }}
      >
        {/* Title bar */}
        <div
          style={{
            height: 44,
            backgroundColor: COLORS.windowChrome,
            borderBottom: `1px solid ${COLORS.windowBorder}`,
            display: "flex",
            alignItems: "center",
            paddingLeft: 16,
            paddingRight: 16,
            flexShrink: 0,
          }}
        >
          {/* Traffic lights */}
          <div style={{ display: "flex", gap: 8 }}>
            <div
              style={{
                width: 12,
                height: 12,
                borderRadius: "50%",
                backgroundColor: "#ff5f57",
              }}
            />
            <div
              style={{
                width: 12,
                height: 12,
                borderRadius: "50%",
                backgroundColor: "#febc2e",
              }}
            />
            <div
              style={{
                width: 12,
                height: 12,
                borderRadius: "50%",
                backgroundColor: "#28c840",
              }}
            />
          </div>
          {/* Title */}
          <div
            style={{
              flex: 1,
              textAlign: "center",
              color: COLORS.textDim,
              fontFamily: FONT.family,
              fontSize: 13,
              fontWeight: 500,
            }}
          >
            Terminal — nostr-cli
          </div>
          <div style={{ width: 52 }} /> {/* Spacer to balance traffic lights */}
        </div>

        {/* Terminal content */}
        <div
          style={{
            flex: 1,
            padding: "20px 24px",
            fontFamily: FONT.family,
            fontSize: FONT.size,
            lineHeight: FONT.lineHeight,
            color: COLORS.text,
            overflow: "hidden",
          }}
        >
          {children}
        </div>
      </div>
    </div>
  );
};
