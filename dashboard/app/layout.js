import './globals.css';
import AuthGate from './AuthGate';

export const metadata = {
  title: 'Adriane · Run Explorer',
  description: 'Kubernetes for AI agents — inspect agent runs',
};

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <body>
        <AuthGate>{children}</AuthGate>
      </body>
    </html>
  );
}
