import { Link } from '@tanstack/react-router'
import type { LinkProps } from '@tanstack/react-router'
import { getUser, logout } from '#/util.ts'
import { useState } from 'react'

type NavlinkProps = {
  href: LinkProps['to']
  text: string
}

function Navlink(props: NavlinkProps) {
  return (
    <Link
      className="hover:text-main transition-colors duration-200 py-1 px-2 rounded-lg hover:bg-gray-100"
      to={props.href}
      inactiveProps={{ className: 'text-gray' }}
    >
      {props.text}
    </Link>
  )
}

export default function Navbar() {
  const [dropdown, setDropdown] = useState(false)
  const user = getUser()

  return (
    <nav className="flex items-center text-lg border rounded-xl mx-auto mt-8 mb-4 px-8 py-2 w-fit gap-8">
      <div className="flex gap-8">
        <Navlink href="/dashboard" text="Dashboard" />
        <Navlink href="/machines" text="Machines" />
        <Navlink href="/jobs" text="Jobs" />
      </div>
      <div className="relative">
        <div
          className="flex items-center cursor-pointer py-1 px-2 rounded-lg hover:bg-gray-100 transition-colors duration-200"
          onClick={() => setDropdown(!dropdown)}
        >
          {user.username}
          <svg
            className="w-5 h-5"
            viewBox="0 0 24 24"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path d="M12 15L7 10H17L12 15Z" fill="#1D1B20" />
          </svg>
          <img
            src={user.profilePicture}
            alt="Profile Picture"
            className="w-7 h-7 rounded-full"
          ></img>
        </div>
        {dropdown && (
          <div className="absolute top-full right-0 border rounded-b m-2 p-1 pr-3 pl-3 text-xl bg-white z-100">
            <ul>
              <li>
                <Link
                  to="/profile"
                  className="block px-2 py-1 rounded hover:bg-gray-100 transition-colors duration-200"
                >
                  Profile
                </Link>
              </li>
              <li>
                <button
                  onClick={logout}
                  className="block w-full text-left px-2 py-1 rounded hover:bg-gray-100 transition-colors duration-200"
                >
                  Logout
                </button>
              </li>
            </ul>
          </div>
        )}
      </div>
    </nav>
  )
}
