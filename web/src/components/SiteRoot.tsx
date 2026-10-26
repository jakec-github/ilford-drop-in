import Logo from "./Logo";
import OrganiserGuide from "./OrganiserGuide";
import "./SiteRoot.css";

// SiteRoot is the app outside every Organisation, when the server leaves the
// base path at the root: the splash at /, the organiser's guide at /guide, and
// "Page not found" anywhere else —
// including an unknown Organisation's address, which must look exactly like
// any other missing page. It fetches nothing: there is no Organisation here to
// ask about, and no session worth checking.
export default function SiteRoot({ pathname }: { pathname: string }) {
  if (pathname === "/") return <Splash />;
  if (pathname === "/guide") return <OrganiserGuide />;
  return <NotFound />;
}

// Splash names, lists and links to no Organisation: an Organisation's address
// is the only way in (ADR 0011). Its one link is the organiser's guide.
function Splash() {
  return (
    <main className="site-root">
      <Logo size={64} />
      <h1>Rota Wise</h1>
      <p>This site runs the rotas for volunteer organisations.</p>
      <p>
        If you volunteer or organise for one, use the link your organisation gave
        you.
      </p>
      <p>
        <a href="/guide">Organiser's guide</a>
      </p>
    </main>
  );
}

function NotFound() {
  return (
    <main className="site-root">
      <Logo size={48} />
      <h1>Page not found</h1>
      <p>
        <a href="/">Go to the Rota Wise home page</a>
      </p>
    </main>
  );
}
