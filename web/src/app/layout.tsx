import type { Metadata } from "next";
import localFont from "next/font/local";
import "./globals.css";
import { Providers } from "./providers";

// Self-hosted latin fonts from Google Fonts (OFL, licences beside them), so
// the build never fetches fonts over the network.
const interTight = localFont({
  src: "./fonts/inter-tight-latin.woff2",
  variable: "--font-inter-tight",
  weight: "400 600",
});

const plexMono = localFont({
  src: [
    { path: "./fonts/ibm-plex-mono-400-latin.woff2", weight: "400" },
    { path: "./fonts/ibm-plex-mono-500-latin.woff2", weight: "500" },
  ],
  variable: "--font-plex-mono",
});

const dmSerif = localFont({
  src: "./fonts/dm-serif-display-latin.woff2",
  variable: "--font-dm-serif",
  weight: "400",
});

// Subset to the single 将 glyph used by the logo mark.
const notoSerifJp = localFont({
  src: "./fonts/noto-serif-jp-mark.woff2",
  variable: "--font-noto-serif-jp",
  weight: "700",
});

export const metadata: Metadata = {
  title: "Shogun",
  description: "Systems for career",
};

const fontVariables = [
  interTight.variable,
  plexMono.variable,
  dmSerif.variable,
  notoSerifJp.variable,
].join(" ");

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${fontVariables} h-full antialiased`}>
      <body className="min-h-full">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
