import { NavLink, Route, Routes, Navigate, useLocation } from "react-router-dom";
import ProductsPage from "./pages/ProductsPage.jsx";
import CombosPage from "./pages/CombosPage.jsx";
import NewOrderPage from "./pages/NewOrderPage.jsx";
import OrdersPage from "./pages/OrdersPage.jsx";
import OrderDetailPage from "./pages/OrderDetailPage.jsx";

const links = [
  { to: "/orders/new", label: "New Order", icon: "🧾" },
  { to: "/orders", label: "Orders", icon: "📦" },
  { to: "/combos", label: "Combo Deals", icon: "🎁" },
  { to: "/products", label: "Products", icon: "💎" }
];

// "/orders" would otherwise match as a prefix of "/orders/new" (and vice versa
// isn't an issue since that route is more specific) — so "Orders" should only
// light up on the list itself or an order detail page, never on "New Order".
function isOrdersLinkActive(pathname) {
  return pathname === "/orders" || /^\/orders\/\d+$/.test(pathname);
}

export default function App() {
  const location = useLocation();

  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="brand">
          <span className="logo">🧾</span>
          <div>
            <h1>CMD Invoice System</h1>
            <div className="sub">Combo deal invoicing</div>
          </div>
        </div>
        <nav>
          {links.map((link) => {
            const isActive =
              link.to === "/orders" ? isOrdersLinkActive(location.pathname) : location.pathname === link.to;
            return (
              <NavLink key={link.to} to={link.to} className={isActive ? "active" : ""} end>
                <span className="icon">{link.icon}</span>
                {link.label}
              </NavLink>
            );
          })}
        </nav>
      </aside>
      <main className="content">
        <Routes>
          <Route path="/" element={<Navigate to="/orders/new" replace />} />
          <Route path="/products" element={<ProductsPage />} />
          <Route path="/combos" element={<CombosPage />} />
          <Route path="/orders/new" element={<NewOrderPage />} />
          <Route path="/orders" element={<OrdersPage />} />
          <Route path="/orders/:id" element={<OrderDetailPage />} />
        </Routes>
      </main>
    </div>
  );
}
