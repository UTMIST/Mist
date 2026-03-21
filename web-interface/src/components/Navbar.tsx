import {Link} from "@tanstack/react-router";
import type {LinkProps} from "@tanstack/react-router";
import {getUser, logout} from "#/util.ts";
import {useState} from "react";

type NavlinkProps = {
  href: LinkProps['to'];
  text: string;
}

function Navlink(props: NavlinkProps) {
  return (
    <Link className="hover:underline hover:text-main" to={props.href} inactiveProps={{ className: "text-gray" }}>
      { props.text }
    </Link>
  )
}

export default function Navbar() {
  const [dropdown, setDropdown] = useState(false);
  const user = getUser();

  return (
    <nav className="flex gap-10 justify-center text-3xl border rounded-xl m-2 p-2">
      <Navlink href="/dashboard" text="Dashboard"/>
      <Navlink href="/machines" text="Machines"/>
      <Navlink href="/jobs" text="Jobs"/>
      <div className="relative">
        <div className="flex items-center cursor-pointer" onClick={() => setDropdown(!dropdown)}>
          { user.username }
          <svg className="w-10 h-10" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M12 15L7 10H17L12 15Z" fill="#1D1B20"/>
          </svg>
          <img src={ user.profilePicture } alt="Profile Picture" className="w-10 h-10"></img>
        </div>
        { dropdown &&
            <div className="absolute top-full right-0 border-1 rounded-b m-2 p-1 pr-3 pl-3 text-xl">
              <ul>
                  <li><Link to="/profile">Profile</Link></li>
                  <li><button onClick={logout}>Logout</button></li>
              </ul>
          </div>
        }
      </div>
    </nav>
  )
}