import { Routes, Route } from "react-router-dom"

import { CreatePastePage } from "@/pages/CreatePastePage"
import { ViewPastePage } from "@/pages/ViewPastePage"
import { LanguageSwitcher } from "@/components/LanguageSwitcher"
import { ThemeSwitcher } from "@/components/ThemeSwitcher"

function App() {
  return (
    <div className="flex min-h-svh items-start justify-center p-4 pt-16 sm:pt-24">
      <div className="fixed top-4 right-4 flex items-center gap-2">
        <ThemeSwitcher />
        <LanguageSwitcher />
      </div>
      <Routes>
        <Route path="/" element={<CreatePastePage />} />
        <Route path="/paste/:id" element={<ViewPastePage />} />
      </Routes>
    </div>
  )
}

export default App
