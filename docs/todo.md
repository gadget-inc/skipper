# TODO

- prometheus metrics
- design doc

## Tests

- controller doesn't assign a function to a pod from an outdated replica set, unless it's the only option

## Deployment Verifier

- has "fusion/deployment" label
- has "fusion/port" annotation
- has "fusion/tenant" does not exist match expression

## UI

- https://philipptanlak.com/web-frontends-in-go/#how-i-structure-my-templates
- display everything fission_summary.ts does

  ```
  => Fission summary

  count | function
  ===================================
  6     | appenv-fn--22129--42940
  5     | appenv-fn--118552--236547
  3     | appenv-fn--26872--52426
  3     | appenv-fn--88586--176051
  3     | appenv-fn--135746--271214
  3     | appenv-fn--177798--356371
  3     | appenv-fn--5818--10351
  2     | appenv-fn--81197--161012
  2     | appenv-fn--25743--50168
  2     | appenv-fn--8931--16577
  2     | appenv-fn--140396--280573
  2     | appenv-fn--93507--185942
  2     | appenv-fn--76800--152183
  2     | appenv-fn--61396--121441
  2     | appenv-fn--113377--226083
  2     | appenv-fn--50580--99842
  2     | oprunner--206463--414789
  2     | oprunner--206462--414787
  1     | appenv-fn--61435--121518
  1     | appenv-fn--57091--112830

  environment     | unspecialized | specialized   | pending
  ==================================================================
  fv-1
      prod       |            10 |            20 |             0
      dev        |             5 |            13 |             0
      oprunner   |             3 |             0 |             0
  fv-2
      prod       |            10 |            28 |             0
      dev        |            10 |            50 |             0
      oprunner   |             5 |             0 |             0
  fv-3
      prod       |            17 |           100 |             3
      dev        |            18 |            90 |             7
      oprunner   |             5 |             0 |             0
  fv-4
      prod       |            24 |           276 |             1
      dev        |            25 |           358 |            10
      oprunner   |             5 |             5 |             0
  ------------------------------------------------------------------
  terminating     |             0 |           185 |
  total           |           137 |           940 |            21
  ```
