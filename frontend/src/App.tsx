import { Routes, Route } from "react-router-dom"

import { CreatePastePage } from "@/pages/CreatePastePage"
import { ViewPastePage } from "@/pages/ViewPastePage"

function App() {
  return (
    <div className="flex min-h-svh items-start justify-center p-4 pt-16 sm:pt-24">
      <Routes>
        <Route path="/" element={<CreatePastePage />} />
        <Route path="/paste/:id" element={<ViewPastePage />} />
      </Routes>
    </div>
  )
}

export default App
